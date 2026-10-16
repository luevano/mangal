package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/selector"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/afs"
	stringutil "github.com/luevano/mangal/v5/util/string"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(libraryCmd)
}

var libraryCmd = &cobra.Command{
	Use:   "library",
	Short: "Manage the downloaded mangas",
	Args:  cobra.NoArgs,
}

var libraryRenameArgs = struct {
	Apply, Refresh, Chapters, Fallback bool
}{}

func init() {
	libraryCmd.AddCommand(libraryRenameCmd)
	libraryRenameCmd.Flags().BoolVar(&libraryRenameArgs.Apply, "apply", false, "Rename, instead of only listing the renames")
	libraryRenameCmd.Flags().BoolVar(&libraryRenameArgs.Refresh, "refresh", false, "Get the AniList metadata again instead of the cached one")
	libraryRenameCmd.Flags().BoolVar(&libraryRenameArgs.Chapters, "chapters", false, "Also rename the chapter files to the chapter name template")
	libraryRenameCmd.Flags().BoolVar(&libraryRenameArgs.Fallback, "fallback", false, "Also rename the folders whose metadata isn't found to the fallback name (drops their year and id tags)")
}

var libraryRenameCmd = &cobra.Command{
	Use:   "rename",
	Short: "Rename manga and provider directories to the current name templates",
	Long: `Rename the manga directories (those with a .libmangal.json, written on download)
and the provider directories in the download path to the names the current name
templates give them, also the directories of a provider's old names. When the directory
of the current name exists already (not empty), the old one's manga folders move into it
one by one: a manga it has already (same provider manga or metadata id) is left for
"mangal library merge", a folder name taken by another manga too, folders of no known
manga stay, and the old directory goes once empty. Only lists the renames unless --apply
is set.

A manga folder's name comes from its library manga's links (the bindings of its sources,
its own ids: what "mangal library bind" sets), which win over the ids of its
.libmangal.json and its name; where they disagree it's listed. --refresh gets the AniList
metadata again (batched) instead of the cached one, e.g. after a title changed there.
--chapters also renames the chapter files to the chapter name template (in their folder;
a name already taken is skipped, as chapters without a recorded title). Folders without a
.libmangal.json aren't renamed ("mangal library scan --mark" marks them). A folder with
metadata ids whose metadata isn't found isn't renamed either (its fallback name would drop
the year and the id tags) and is listed as a warning; --fallback renames it anyway.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		st := newStatus(cmd)
		defer st.Done()
		c, err := newCore()
		if err != nil {
			return err
		}
		ctx := changeCtx(cmd, st)
		p, err := c.Changes.Plan(ctx, core.RenameReq{
			Refresh:  libraryRenameArgs.Refresh,
			Chapters: libraryRenameArgs.Chapters,
			Fallback: libraryRenameArgs.Fallback,
		})
		if err != nil {
			return err
		}
		plan := p.Detail.(client.RenamePlan)
		rel, renames := plan.Rel, plan.Renames
		st.Clear()
		if len(plan.Disagree) != 0 {
			cmd.Println("ids that disagree with the links (the links are used, \"mangal library bind <id>\" shows and fixes them):")
			for _, line := range plan.Disagree {
				cmd.Println("  " + line)
			}
		}
		if plan.Unmarked != 0 {
			cmd.Printf("%d folders without a %s aren't renamed (\"mangal library scan --mark\" marks them)\n", plan.Unmarked, libmangal.FilenameMangaDirInfo)
		}
		if plan.Untitled != 0 {
			cmd.Printf("%d chapter files kept: no chapter title recorded\n", plan.Untitled)
		}
		if noMetadata := plan.NoMetadata(); len(noMetadata) != 0 {
			cmd.Printf("warning: %d folders not renamed, no metadata found for their ids (the fallback name would drop the year and ids; --fallback renames them anyway):\n", len(noMetadata))
			for _, r := range noMetadata {
				cmd.Printf("  %s: %s (would be %s)\n", rel(r.From), r.Skip, filepath.Base(r.To))
			}
		}
		if len(renames) == 0 {
			cmd.Println("nothing to rename")
			return nil
		}

		if !libraryRenameArgs.Apply {
			printLines(cmd, p.Lines)
			cmd.Println("run with --apply to rename")
			return nil
		}
		if p.Nothing {
			printLines(cmd, p.Lines)
			return nil
		}
		opts := applyOpts(st)
		opts.OnLine = func(l core.Line) { st.Print(func() { printResultLines(cmd, []core.Line{l}) }) }
		res, err := c.Changes.ApplyPlan(ctx, p, opts)
		st.Done()
		undoHint(cmd, res.Snapshot, err)
		return err
	},
}

func init() {
	libraryCmd.AddCommand(libraryDownloadsCmd)
}

var libraryDownloadsCmd = &cobra.Command{
	Use:   "downloads [provider [manga-id]]",
	Short: "List the downloaded chapters",
	Long: `List the downloaded chapters, grouped by manga, or the chapters of one provider or manga
(provider and manga ids as shown in the list).`,
	Args:              cobra.MaximumNArgs(2),
	ValidArgsFunction: completionFirstProvider,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newCore()
		if err != nil {
			return err
		}
		provider, mangaID := providerManga(args)
		downloads, err := c.Library.ProviderDownloads(ctxOf(cmd), provider, mangaID)
		if err != nil {
			return err
		}
		if len(downloads) == 0 {
			cmd.Println("no downloads")
			return nil
		}

		if mangaID != "" {
			for _, dl := range downloads {
				cmd.Printf("vol %g ch %g  %s  %s  %d pages  %s\n", dl.Volume, dl.Chapter, dl.ChapterTitle, dl.Format, dl.Pages, dl.Path)
			}
			return nil
		}

		type manga struct {
			provider, id, title string
			chapters            int
			last                time.Time
		}
		var mangas []*manga
		for _, dl := range downloads {
			if n := len(mangas); n == 0 || mangas[n-1].provider != dl.Provider || mangas[n-1].id != dl.MangaID {
				mangas = append(mangas, &manga{provider: dl.Provider, id: dl.MangaID, title: dl.MangaTitle})
			}
			m := mangas[len(mangas)-1]
			m.chapters++
			if dl.Downloaded.After(m.last) {
				m.last = dl.Downloaded
			}
		}
		for _, m := range mangas {
			cmd.Printf("%s  %s %s  %d chapters, last %s\n", m.title, m.provider, m.id, m.chapters, m.last.Format(time.DateOnly))
		}
		return nil
	},
}

func init() {
	libraryCmd.AddCommand(libraryLsCmd)
	libraryCmd.AddCommand(libraryScanCmd)
	libraryScanCmd.Flags().BoolVar(&libraryScanArgs.Check, "check", false, "Check the metadata ids of the folders without a .libmangal.json on AniList and Kitsu")
	libraryScanCmd.Flags().BoolVar(&libraryScanArgs.Mark, "mark", false, "List the folders without a .libmangal.json that can get one, from what's known")
	libraryScanCmd.Flags().BoolVar(&libraryScanArgs.Apply, "apply", false, "With --mark, write the .libmangal.json files instead of only listing them")
	libraryCmd.AddCommand(libraryMergeCmd)
	libraryMergeCmd.Flags().BoolVar(&libraryMergeArgs.Apply, "apply", false, "Merge, instead of only listing what would happen")
	libraryCmd.AddCommand(libraryMoveCmd)
	libraryMoveCmd.Flags().BoolVar(&libraryMoveArgs.Apply, "apply", false, "Move, instead of only listing what would happen")
	libraryMoveCmd.Flags().BoolVar(&libraryMoveArgs.Full, "full", false, "Fully migrate: adopt the new provider's title and the folder name it gives")
	libraryMoveCmd.Flags().StringVarP(&libraryMoveArgs.Selector, "manga-selector", "m", "exact", "Manga selector of the search (first|last|exact|closest|<index>)")
	libraryCmd.AddCommand(libraryTrashCmd)
	libraryTrashCmd.AddCommand(libraryTrashEmptyCmd)
	libraryTrashEmptyCmd.Flags().BoolVarP(&libraryTrashEmptyArgs.Yes, "yes", "y", false, "Empty it without asking")
}

var libraryLsArgs = struct {
	Refresh bool
}{}

func init() {
	libraryLsCmd.Flags().BoolVar(&libraryLsArgs.Refresh, "refresh", false, "Look the publication statuses up on AniList (batched): the unknown ones and those not looked up lately")
}

var libraryLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List the mangas in the library",
	Long: `List the mangas in the library: id, title, chapters, publication status (finished,
ongoing, hiatus, cancelled, upcoming; from the AniList metadata already cached, --refresh
asks AniList), folder and sources.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		c, err := newCore()
		if err != nil {
			return err
		}
		st := newStatus(cmd)
		list, err := c.Library.Mangas(changeCtx(cmd, st), core.MangasOptions{Statuses: true, Fetch: libraryLsArgs.Refresh})
		st.Done()
		if err != nil {
			return err
		}
		mangas := list.Mangas
		if list.StatusError != "" {
			if libraryLsArgs.Refresh {
				return errors.New(list.StatusError)
			}
			cmd.PrintErrf("warning: publication statuses: %s\n", list.StatusError)
		}
		if len(mangas) == 0 {
			cmd.Println(`the library is empty, "mangal library scan" adds the mangas already downloaded`)
		}
		for _, m := range mangas {
			line := fmt.Sprintf("%d  %s  %d chapters  ", m.ID, m.Title, m.Chapters)
			if status := library.StatusLabel(m.Status); status != "" {
				line += status + "  "
			}
			line += m.Path()
			if sources := library.SourcesSummary(m.Sources); sources != "" {
				line += "  [" + sources + "]"
			}
			cmd.Println(line)
		}
		printTrash(cmd)
		return nil
	},
}

// printTrash tells about files waiting in the trash.
func printTrash(cmd *cobra.Command) {
	batches, err := library.Trash(afs.Afero, path.Downloads())
	if err != nil || len(batches) == 0 {
		return
	}
	var files int
	var size int64
	for _, b := range batches {
		files += b.Files
		size += b.Size
	}
	cmd.Printf("\n%d files (%s) in the trash, \"mangal library trash empty\" deletes them\n", files, stringutil.HumanSize(size))
}

var libraryScanArgs = struct {
	Check bool
	Mark  bool
	Apply bool
}{}

var libraryScanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Check the library against the files",
	Long: `Check the library against the downloaded files: moved manga folders and chapter files
are found again, deleted ones forgotten, files nobody recorded (e.g. copied by hand) added,
and manga folders that aren't in the library yet are added: with a .libmangal.json, or
else guessed from their series.json, chapters and name (with the metadata id of an
"[alid-N]" tag). It ends with the library mangas that share a metadata id (AniList,
Kitsu, MyAnimeList): the same manga in two folders, with the one to keep suggested
(watched or linked to a provider first, then more chapters) and the merge command.

--check looks the guessed ids up on AniList and Kitsu and lists those not found or
whose manga doesn't look like the folder's, and suggests an AniList id for the folders
without any. It only reports, a download from a provider links the folder for good.

--mark lists the manga folders without a .libmangal.json that --mark --apply writes one
(as downloads do) into,
from what's known: the library's source of it (provider and manga id, also found from
its watch or recorded downloads), else the provider of the provider directory it's in
(no manga id), else only its title and metadata ids, so downloads, scans and renames
recognize the folder by it. Folders with nothing to go by are left as they are, an
existing .libmangal.json is never replaced.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if libraryScanArgs.Apply && !libraryScanArgs.Mark {
			return errors.New("--apply goes with --mark")
		}
		st := newStatus(cmd)
		defer st.Done()
		c, err := newCore()
		if err != nil {
			return err
		}
		ctx := changeCtx(cmd, st)
		rep, err := c.Library.Scan(ctx, core.ScanOptions{})
		st.Clear()
		if err != nil {
			return err
		}

		// mangas in another (existing) download directory
		if dir := rep.OtherRoot; dir != "" &&
			confirm(cmd, fmt.Sprintf("%d mangas are in %s, but download.path is %s. Use %s as download.path?", len(rep.Result.Missing), dir, rep.Root, dir)) {
			if err := config.WriteKey(config.Download.Path.Key, dir); err != nil {
				return err
			}
			rep, err = c.Library.Scan(ctx, core.ScanOptions{})
			st.Clear()
			if err != nil {
				return err
			}
		}
		res := rep.Result

		cmd.Printf("added %d, moved %d, removed %d chapters; found %d moved manga folders, %d new\n",
			res.Added, res.Moved, res.Removed, res.Relocated, res.New)
		if n := res.Sources.FromWatches + res.Sources.FromDownloads; n > 0 {
			cmd.Printf("%d sources found from watches/downloads (%d, %d)\n", n, res.Sources.FromWatches, res.Sources.FromDownloads)
		}
		if len(res.Unrecognized) != 0 {
			cmd.Printf("%d files without a chapter number:\n", len(res.Unrecognized))
			for _, f := range res.Unrecognized {
				cmd.Println("  " + f)
			}
		}
		if len(res.Missing) != 0 {
			cmd.Printf("%d mangas weren't found:\n", len(res.Missing))
			for _, m := range res.Missing {
				cmd.Printf("  %d  %s  %s\n", m.ID, m.Title, m.Path())
			}
			if confirm(cmd, "Remove them from the library?") {
				if err := forgetMissing(cmd, st, res.Missing); err != nil {
					return err
				}
				cmd.Println("removed")
				if rep.Duplicates, err = c.Library.Duplicates(ctx); err != nil {
					return err
				}
			}
		}
		printLines(cmd, library.DuplicateLines(rep.Duplicates, rep.Root))
		if libraryScanArgs.Mark {
			if err := markFolders(cmd, st, libraryScanArgs.Apply); err != nil {
				return err
			}
		}
		if libraryScanArgs.Check {
			if err := checkIDs(cmd, st, c); err != nil {
				return err
			}
		}
		st.Done()
		printTrash(cmd)
		return nil
	},
}

// forgetMissing forgets the library mangas whose folders are gone, as one operation.
func forgetMissing(cmd *cobra.Command, st *status, missing []storage.Manga) error {
	c, err := newCore()
	if err != nil {
		return err
	}
	ids := make([]int64, len(missing))
	for i, m := range missing {
		ids[i] = m.ID
	}
	res, err := c.Changes.Apply(changeCtx(cmd, st), core.ForgetReq{Mangas: ids}, applyOpts(st))
	undoHint(cmd, res.Snapshot, err)
	return err
}

// markFolders writes (apply) or lists the missing .libmangal.json of the folders, from
// what's known.
func markFolders(cmd *cobra.Command, st *status, apply bool) error {
	c, err := newCore()
	if err != nil {
		return err
	}
	ctx := changeCtx(cmd, st)
	count := "to mark"
	if apply {
		count = "marked"
	}
	plan, err := c.Changes.Plan(ctx, core.MarkReq{})
	if err != nil {
		return err
	}
	res := plan.Detail.(core.MarkDetail).Result
	if !apply {
		st.Print(func() { printLines(cmd, plan.Lines) })
	}
	snapshot := ""
	if apply && !plan.Nothing {
		opts := applyOpts(st)
		opts.OnLine = func(l core.Line) { st.Print(func() { cmd.Println(l.Text) }) }
		var done core.OpResult
		done, err = c.Changes.ApplyPlan(ctx, plan, opts)
		snapshot = done.Snapshot
		if r, ok := done.Detail.(library.MarkResult); ok {
			res = r
		}
	}
	st.Clear()
	undoHint(cmd, snapshot, err)
	if err != nil {
		return err
	}
	counts := map[library.MarkEvidence]int{}
	for _, m := range res.Marked {
		counts[m.Evidence]++
	}
	cmd.Printf("%d %s (%d from the library source, %d provider from its directory, %d metadata ids only), %d with nothing to go by (not marked)\n",
		len(res.Marked), count, counts[library.MarkedFromSource], counts[library.MarkedFromDir], counts[library.MarkedFromIDs], len(res.Unknown))
	if len(res.Broken) != 0 {
		cmd.Printf("%d folders have a broken %s, left as they are (\"mangal library mark <id>\" replaces one):\n", len(res.Broken), libmangal.FilenameMangaDirInfo)
		for _, m := range res.Broken {
			cmd.Printf("  %d  %s\n", m.ID, m.Path())
		}
	}
	if !apply && len(res.Marked) != 0 {
		cmd.Println("run with --mark --apply to write them")
	}
	return nil
}

var libraryMarkArgs = struct {
	Apply bool
}{}

func init() {
	libraryCmd.AddCommand(libraryMarkCmd)
	libraryMarkCmd.Flags().BoolVar(&libraryMarkArgs.Apply, "apply", false, "Write the markers, instead of only listing them")
}

var libraryMarkCmd = &cobra.Command{
	Use:   "mark <id>...",
	Short: "Write the .libmangal.json of library mangas",
	Long: `Write the .libmangal.json of these library mangas (ids from "mangal library ls") from
what the library knows, as "mangal library scan --mark" does for every folder without one.
Unlike it, a .libmangal.json that's broken (invalid JSON) or says another provider manga
than the library's sources (or has no manga id) is replaced, keeping its metadata ids of
other sources; one that agrees is left. Only lists what it would write unless --apply.`,
	Example: `  mangal library mark 12
  mangal library mark 12 15 --apply`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var ids []int64
		for _, arg := range args {
			id, err := parseMangaID(arg)
			if err != nil {
				return err
			}
			ids = append(ids, id)
		}
		c, err := newCore()
		if err != nil {
			return err
		}
		st := newStatus(cmd)
		defer st.Done()
		ctx := changeCtx(cmd, st)
		plan, err := c.Changes.Plan(ctx, core.MarkReq{Mangas: ids})
		st.Clear()
		if err != nil {
			return err
		}
		for _, line := range plan.Lines {
			if libraryMarkArgs.Apply && strings.HasPrefix(line, "would mark ") {
				continue
			}
			cmd.Println(line)
		}
		if plan.Nothing {
			return nil
		}
		if !libraryMarkArgs.Apply {
			cmd.Println("run with --apply to write them")
			return nil
		}
		opts := applyOpts(st)
		opts.OnLine = func(l core.Line) { st.Print(func() { cmd.Println(l.Text) }) }
		res, err := c.Changes.ApplyPlan(ctx, plan, opts)
		st.Done()
		undoHint(cmd, res.Snapshot, err)
		return err
	},
}

// checkIDs reports the metadata ids of the adopted folders that look wrong.
func checkIDs(cmd *cobra.Command, st *status, c *core.Core) error {
	checks, err := c.Library.CheckIDs(changeCtx(cmd, st))
	st.Clear()
	if err != nil {
		return err
	}
	if len(checks) == 0 {
		cmd.Println("the ids of the folders without a .libmangal.json look right")
		return nil
	}
	cmd.Printf("check the ids of %d folders without a .libmangal.json:\n", len(checks))
	for _, c := range checks {
		cmd.Println("  " + c.String())
	}
	return nil
}

var libraryMergeArgs = struct {
	Apply bool
}{}

var libraryMergeCmd = &cobra.Command{
	Use:   "merge <keep> <other>",
	Short: "Merge two library mangas (e.g. the same manga from two providers)",
	Long: `Merge the library manga other into keep (ids from "mangal library ls"): other's chapters
that keep doesn't have are moved into keep's folder, the ones both have stay keep's and
other's copy goes to the trash, as does what's left of other's folder.

Only lists what would happen unless --apply is set.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		var ids [2]int64
		for i, arg := range args {
			id, err := parseMangaID(arg)
			if err != nil {
				return err
			}
			ids[i] = id
		}
		c, err := newCore()
		if err != nil {
			return err
		}
		st := newStatus(cmd)
		defer st.Done()
		ctx := changeCtx(cmd, st)
		plan, err := c.Changes.Plan(ctx, core.MergeReq{Keep: ids[0], Other: ids[1]})
		if err != nil {
			return err
		}
		printLines(cmd, plan.Lines)
		if !libraryMergeArgs.Apply {
			cmd.Println("run with --apply to merge")
			return nil
		}
		res, err := c.Changes.ApplyPlan(ctx, plan, applyOpts(st))
		st.Done()
		undoHint(cmd, res.Snapshot, err)
		if err != nil {
			return err
		}
		cmd.Println("merged")
		printTrash(cmd)
		return nil
	},
}

var libraryMoveArgs = struct {
	Apply    bool
	Full     bool
	Selector string
}{}

var libraryMoveCmd = &cobra.Command{
	Use:   "move <id> <provider> [query|mid:id]",
	Short: "Move a library manga to another provider",
	Long: `Move the library manga (id from "mangal library ls") to a manga of another provider: the
one with the id ("mid:<id>"), or the one the manga selector picks from a search of the
query (without a query, the library manga's title, an exact match).

The library manga gets that manga as a source (its other sources stay), its folder moves
into that provider's directory (with download.provider.create_dir) under the same name
with its .libmangal.json rewritten, its watch is re-linked to it and it gets the metadata
bindings it hasn't got. The library title and the folder's name stay; --full adopts the
new manga's title and the folder name the manga name template gives it (unchanged when
the template uses the AniList title). A manga that's another library manga's already is
an error: merge them instead.

Only lists what would happen unless --apply is set.`,
	Example: `  mangal library move 12 mango-mangapill
  mangal library move 12 mango-weebcentral "Dungeon Odyssey" --apply
  mangal library move 12 mango-weebcentral --full
  mangal library move 12 mango-mangadex mid:7c145eb7-2ee0-4a82-8d5c-bbb7ba0a4d42`,
	Args:              cobra.MinimumNArgs(2),
	ValidArgsFunction: completionMoveArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		st := newStatus(cmd)
		defer st.Done()
		ctx := st.Context(cmd.Context())
		if _, err := path.ExistingDownloads(); err != nil {
			return err
		}
		c, err := newCore()
		if err != nil {
			return err
		}
		id, err := parseMangaID(args[0])
		if err != nil {
			return err
		}
		manga, err := c.Library.Manga(ctx, id)
		if err != nil {
			return err
		}
		st.Set("loading " + args[1])
		cl, warnings, err := client.AcquireByID(ctx, args[1])
		if err != nil {
			return err
		}
		for _, w := range warnings {
			st.Print(func() { cmd.PrintErrf("warning: %s\n", w) })
		}
		query := strings.Join(args[2:], " ")
		if query == "" {
			query = manga.Title
		}
		if !strings.HasPrefix(query, "mid:") {
			st.Set(fmt.Sprintf("searching %q on %s", query, cl.Info().Name))
		}
		to, err := selector.Find(ctx, client.Searcher(cl), "", query, libraryMoveArgs.Selector)
		if err != nil {
			return err
		}
		st.Set("planning the move")
		ctx = core.WithProgress(ctx, st.Progress)
		plan, err := c.Changes.Plan(ctx, core.MoveReq{Manga: id, Provider: cl.Info(), To: to, Full: libraryMoveArgs.Full})
		st.Done()
		if err != nil {
			return err
		}
		printLines(cmd, plan.Lines)
		if !libraryMoveArgs.Apply {
			cmd.Println("run with --apply to move")
			return nil
		}
		res, err := c.Changes.ApplyPlan(ctx, plan, applyOpts(st))
		undoHint(cmd, res.Snapshot, err)
		if err != nil {
			return err
		}
		cmd.Println("moved")
		return nil
	},
}

var libraryTrashCmd = &cobra.Command{
	Use:   "trash",
	Short: "List what's in the trash",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		batches, err := library.Trash(afs.Afero, path.Downloads())
		if err != nil {
			return err
		}
		if len(batches) == 0 {
			cmd.Println("the trash is empty")
			return nil
		}
		for _, b := range batches {
			cmd.Printf("%s  %d files  %s  %s\n", b.Name, b.Files, stringutil.HumanSize(b.Size), b.Path)
		}
		return nil
	},
}

var libraryTrashEmptyArgs = struct {
	Yes bool
}{}

var libraryTrashEmptyCmd = &cobra.Command{
	Use:   "empty",
	Short: "Delete what's in the trash",
	Long: `Delete what's in the trash, for good. The operations that put files there can't be
undone after it ("mangal library undo"): they're listed before asking (--yes doesn't ask;
without a terminal it's needed).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		c, err := newCore()
		if err != nil {
			return err
		}
		st := newStatus(cmd)
		defer st.Done()
		plan, err := c.Changes.Plan(ctxOf(cmd), core.EmptyTrashReq{})
		if err != nil {
			return err
		}
		if plan.Nothing {
			cmd.Println("the trash is empty")
			return nil
		}
		trash := plan.Detail.(core.EmptyTrashPlan)
		printLines(cmd, trash.BlockedLines())
		if !libraryTrashEmptyArgs.Yes && !confirm(cmd, fmt.Sprintf("Delete the %d files in the trash?", trash.Files)) {
			return nil
		}
		res, err := c.Changes.ApplyPlan(ctxOf(cmd), plan, applyOpts(st))
		st.Done()
		if res.Snapshot != "" {
			cmd.PrintErrf("database snapshot: %s\n", res.Snapshot)
		}
		if err != nil {
			return err
		}
		cmd.Println("emptied (can't be undone)")
		return nil
	},
}

// undoHint tells where the database snapshot of an operation went and how to undo it.
func undoHint(cmd *cobra.Command, snapshot string, err error) {
	switch {
	case snapshot == "":
	case err != nil:
		cmd.PrintErrf("database snapshot: %s, \"mangal library undo\" undoes what was done\n", snapshot)
	default:
		cmd.PrintErrf("database snapshot: %s, \"mangal library undo\" undoes it\n", snapshot)
	}
}

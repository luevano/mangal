package cmd

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/client/kitsu"
	"github.com/luevano/mangal/v5/client/selector"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/template"
	"github.com/luevano/mangal/v5/util/afs"
	"github.com/luevano/mangal/v5/util/cache"
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
		index, err := cache.Library()
		if err != nil {
			return err
		}
		dir, err := path.ExistingDownloads()
		if err != nil {
			return err
		}
		rel := func(p string) string {
			if r, err := filepath.Rel(dir, p); err == nil {
				return r
			}
			return p
		}
		var disagree []string
		namer, err := client.LibraryNamer(index, anilist.MetadataByIDsBatch)
		if err != nil {
			return err
		}
		namer.OnProgress = st.Progress
		namer.OnDisagree = func(folder string, d []string) {
			for _, line := range d {
				disagree = append(disagree, fmt.Sprintf("  %s: %s", rel(folder), line))
			}
		}
		if libraryRenameArgs.Refresh {
			namer.Metadata = anilist.MetadataByIDsFresh
		}
		namer.Fallback = libraryRenameArgs.Fallback
		providerDirs := config.Download.Provider.CreateDir.Get()
		renames, err := namer.Plan(st.Context(cmd.Context()), dir, providerDirs)
		if err != nil {
			return err
		}
		untitled := 0
		if libraryRenameArgs.Chapters {
			mangas, err := index.Mangas()
			if err != nil {
				return err
			}
			chapters, err := namer.PlanChapters(mangas)
			if err != nil {
				return err
			}
			var kept []library.Rename
			for _, r := range chapters {
				if r.Skip == "no chapter title recorded" {
					untitled++
					continue
				}
				kept = append(kept, r)
			}
			// before the folders: they use the paths of now
			renames = append(kept, renames...)
		}
		st.Clear()
		if len(disagree) != 0 {
			cmd.Println("ids that disagree with the links (the links are used, \"mangal library bind <id>\" shows and fixes them):")
			for _, line := range disagree {
				cmd.Println(line)
			}
		}
		if unmarked, err := library.Unmarked(afs.Afero, dir, providerDirs); err == nil && len(unmarked) != 0 {
			cmd.Printf("%d folders without a %s aren't renamed (\"mangal library scan --mark\" marks them)\n", len(unmarked), libmangal.FilenameMangaDirInfo)
		}
		if untitled != 0 {
			cmd.Printf("%d chapter files kept: no chapter title recorded\n", untitled)
		}
		var noMetadata []library.Rename
		for _, r := range renames {
			if r.NoMetadata && r.Skip != "" {
				noMetadata = append(noMetadata, r)
			}
		}
		if len(noMetadata) != 0 {
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
			for _, r := range renames {
				switch {
				case r.NoMetadata && r.Skip != "":
					// listed in the warning above
				case r.Skip != "":
					cmd.Printf("skip %s: %s\n", rel(r.From), r.Skip)
				case r.RemoveEmpty:
					cmd.Printf("remove %s once empty\n", rel(r.From))
				case r.Chapter:
					cmd.Printf("%s -> %s\n", rel(r.From), filepath.Base(r.To))
				default:
					cmd.Printf("%s -> %s\n", rel(r.From), rel(r.To))
				}
			}
			cmd.Println("run with --apply to rename")
			return nil
		}
		folders, files := 0, 0
		for _, r := range renames {
			switch {
			case r.Skip != "" || r.RemoveEmpty:
			case r.Chapter:
				files++
			default:
				folders++
			}
		}
		snapshot, err := client.Operation("rename", renameSummary(folders, files), func(j library.Journal) error {
			// the journal says what was renamed, not what was planned
			folders, files := 0, 0
			if op, ok := j.(interface{ SetSummary(string) }); ok {
				defer func() { op.SetSummary(renameSummary(folders, files)) }()
			}
			return library.ApplyRenamesContext(cmd.Context(), afs.Afero, client.LibraryModes(), index, j, renames, st.Progress, func(i int, err error) {
				r := renames[i]
				switch {
				case errors.Is(err, library.ErrNotEmpty):
					st.Print(func() { cmd.Printf("kept %s: not empty\n", rel(r.From)) })
				case err != nil:
					st.Print(func() { cmd.PrintErrf("skipped %s: %s\n", rel(r.From), err) })
				case r.RemoveEmpty:
					st.Print(func() { cmd.Printf("removed %s\n", rel(r.From)) })
				case r.Chapter:
					files++
					st.Print(func() { cmd.Printf("%s -> %s\n", rel(r.From), filepath.Base(r.To)) })
				default:
					folders++
					st.Print(func() { cmd.Printf("%s -> %s\n", rel(r.From), rel(r.To)) })
				}
			})
		})
		st.Done()
		undoHint(cmd, snapshot, err)
		return err
	},
}

// renameSummary is the journal's summary of a rename.
func renameSummary(folders, files int) string {
	var what []string
	if folders != 0 || files == 0 {
		what = append(what, fmt.Sprintf("%d folders", folders))
	}
	if files != 0 {
		what = append(what, fmt.Sprintf("%d chapter files", files))
	}
	return "rename " + strings.Join(what, ", ")
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
	ValidArgsFunction: completionProviderIDs,
	RunE: func(cmd *cobra.Command, args []string) error {
		lib, err := cache.Library()
		if err != nil {
			return err
		}
		provider, mangaID := providerManga(args)
		downloads, err := lib.ListDownloads(provider, mangaID)
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

// libraryFiles is client.LibraryFiles, an error when download.path doesn't exist.
func libraryFiles() (library.Files, error) {
	if _, err := path.ExistingDownloads(); err != nil {
		return library.Files{}, err
	}
	return client.LibraryFiles()
}

// libraryOf opens the library of the data database.
func libraryOf() (storage.Library, error) {
	return cache.Library()
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
		lib, err := libraryOf()
		if err != nil {
			return err
		}
		mangas, err := lib.Mangas()
		if err != nil {
			return err
		}
		st := newStatus(cmd)
		err = client.UpdateLibraryStatuses(st.Context(cmd.Context()), lib, mangas, libraryLsArgs.Refresh, st.Progress)
		st.Done()
		if err != nil {
			if libraryLsArgs.Refresh {
				return err
			}
			cmd.PrintErrf("warning: publication statuses: %s\n", err)
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
		lib, err := libraryOf()
		if err != nil {
			return err
		}
		root, err := path.ExistingDownloads()
		if err != nil {
			return err
		}
		providerDirs := config.Download.Provider.CreateDir.Get()
		scanner := library.Scanner{FS: afs.Afero, Library: lib, OnProgress: st.Progress}
		res, err := scanner.ScanContext(cmd.Context(), root, providerDirs)
		st.Clear()
		if err != nil {
			return err
		}

		// mangas in another (existing) download directory
		if dir := library.OtherRoot(afs.Afero, res.Missing, root, providerDirs); dir != "" &&
			confirm(cmd, fmt.Sprintf("%d mangas are in %s, but download.path is %s. Use %s as download.path?", len(res.Missing), dir, root, dir)) {
			if err := config.WriteKey(config.Download.Path.Key, dir); err != nil {
				return err
			}
			root = dir
			res, err = scanner.ScanContext(cmd.Context(), root, providerDirs)
			st.Clear()
			if err != nil {
				return err
			}
		}

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
				snapshot, err := client.RemoveMissing(lib, res.Missing)
				undoHint(cmd, snapshot, err)
				if err != nil {
					return err
				}
				cmd.Println("removed")
			}
		}
		if err := printDuplicates(cmd, lib, root); err != nil {
			return err
		}
		if libraryScanArgs.Mark {
			if err := markFolders(cmd, st, lib, root, libraryScanArgs.Apply); err != nil {
				return err
			}
		}
		if libraryScanArgs.Check {
			if err := checkIDs(cmd, st, lib); err != nil {
				return err
			}
		}
		st.Done()
		printTrash(cmd)
		return nil
	},
}

// printDuplicates lists the library mangas that share a metadata id.
func printDuplicates(cmd *cobra.Command, lib storage.Library, root string) error {
	dups, err := client.LibraryDuplicates(lib)
	if err != nil {
		return err
	}
	for _, line := range library.DuplicateLines(dups, root) {
		cmd.Println(line)
	}
	return nil
}

// markFolders writes (apply) or lists the missing .libmangal.json of the folders, from
// what's known.
func markFolders(cmd *cobra.Command, st *status, lib storage.Library, root string, apply bool) error {
	loaders, err := manager.Loaders()
	if err != nil {
		return err
	}
	files := library.Files{FS: afs.Afero, Library: lib, Root: root, Modes: client.LibraryModes(), OnProgress: st.Progress}
	verb, count := "would mark", "to mark"
	if apply {
		verb, count = "marked", "marked"
	}
	var res library.MarkResult
	mark := func(j library.Journal, dryRun, tell bool) error {
		files.Journal = j
		res, err = files.MarkContext(cmd.Context(), library.MarkOptions{
			Loaders: loaders, ProviderDirs: config.Download.Provider.CreateDir.Get(), ProviderName: template.Provider,
			DryRun: dryRun,
			OnMarked: func(m library.Marked) {
				if !tell {
					return
				}
				as := cmp.Or(m.Info.Provider, "no provider")
				if m.Info.Manga.ID != "" {
					as += " " + m.Info.Manga.ID
				}
				st.Print(func() { cmd.Printf("%s %s as %s (%s)\n", verb, m.Manga.Path(), as, m.Evidence) })
			},
		})
		return err
	}
	// a dry run first: no snapshot nor journal when there's nothing to mark
	err = mark(nil, true, !apply)
	snapshot := ""
	if apply && err == nil && len(res.Marked) != 0 {
		snapshot, err = client.Operation("mark", fmt.Sprintf("mark %d folders", len(res.Marked)), func(j library.Journal) error {
			return mark(j, false, true)
		})
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
		files, err := libraryFiles()
		if err != nil {
			return err
		}
		opts, err := client.LibraryMarkOptions()
		if err != nil {
			return err
		}
		var mangas []storage.Manga
		for _, arg := range args {
			id, err := strconv.ParseInt(arg, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid manga id %q", arg)
			}
			m, ok, err := files.Library.MangaByID(id)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("no manga %d in the library", id)
			}
			mangas = append(mangas, m)
		}
		verb := "would mark"
		mark := func(dryRun bool) (int, error) {
			n := 0
			opts.DryRun = dryRun
			for _, m := range mangas {
				marked, done, err := files.MarkManga(m, opts)
				if err != nil {
					return n, err
				}
				if !done {
					if dryRun {
						cmd.Printf("%d %s: nothing to do (its %s agrees, or no folder, or nothing to go by)\n", m.ID, m.Title, libmangal.FilenameMangaDirInfo)
					}
					continue
				}
				n++
				as := cmp.Or(marked.Info.Provider, "no provider")
				if marked.Info.Manga.ID != "" {
					as += " " + marked.Info.Manga.ID
				}
				if dryRun == !libraryMarkArgs.Apply {
					cmd.Printf("%s %s as %s (%s)\n", verb, m.Path(), as, marked.Evidence)
				}
			}
			return n, nil
		}
		n, err := mark(true)
		if err != nil || n == 0 {
			return err
		}
		if !libraryMarkArgs.Apply {
			cmd.Println("run with --apply to write them")
			return nil
		}
		verb = "marked"
		snapshot, err := client.Operation("mark", fmt.Sprintf("mark %d folders", n), func(j library.Journal) error {
			files.Journal = j
			_, err := mark(false)
			return err
		})
		undoHint(cmd, snapshot, err)
		return err
	},
}

// checkIDs reports the metadata ids of the adopted folders that look wrong.
func checkIDs(cmd *cobra.Command, st *status, lib storage.Library) error {
	mangas, err := lib.Mangas()
	if err != nil {
		return err
	}
	ctx := st.Context(cmd.Context())
	checker, err := idChecker(ctx)
	if err != nil {
		return err
	}
	checker.OnProgress = st.Progress
	checks, err := checker.Check(ctx, mangas)
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

// idChecker checks AniList and Kitsu ids, and suggests AniList ones.
func idChecker(ctx context.Context) (library.IDChecker, error) {
	ani, err := anilist.Anilist()
	if err != nil {
		return library.IDChecker{}, err
	}
	_ = anilist.EnsureLogin(ctx)
	checker := library.IDChecker{
		FS:        afs.Afero,
		Providers: map[metadata.IDSource]library.IDProvider{metadata.IDSourceAnilist: ani},
		Suggest:   ani,
	}
	kt, err := kitsu.Kitsu()
	if err != nil {
		return library.IDChecker{}, err
	}
	checker.Providers[metadata.IDSourceKitsu] = kt
	return checker, nil
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
		lib, err := libraryOf()
		if err != nil {
			return err
		}
		var mangas [2]storage.Manga
		for i, arg := range args {
			id, err := strconv.ParseInt(arg, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid manga id %q", arg)
			}
			m, ok, err := lib.MangaByID(id)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("no manga %d in the library", id)
			}
			mangas[i] = m
		}
		files, err := libraryFiles()
		if err != nil {
			return err
		}
		st := newStatus(cmd)
		defer st.Done()
		files.OnProgress = st.Progress
		plan, err := files.PlanMerge(mangas[0], mangas[1])
		if err != nil {
			return err
		}
		cmd.Printf("keep %q (%s), merge %q (%s) into it:\n", plan.Keep.Title, plan.Keep.Path(), plan.Other.Title, plan.Other.Path())
		for _, m := range plan.Moves {
			cmd.Printf("  move   %s\n", filepath.Base(m.From))
		}
		for _, p := range plan.Trashed {
			cmd.Printf("  trash  %s (already in %q)\n", filepath.Base(p), plan.Keep.Title)
		}
		if !libraryMergeArgs.Apply {
			cmd.Println("run with --apply to merge")
			return nil
		}
		snapshot, err := client.Operation("merge", fmt.Sprintf("merge %d %q into %d %q", plan.Other.ID, plan.Other.Title, plan.Keep.ID, plan.Keep.Title), func(j library.Journal) error {
			files.Journal = j
			return files.ApplyMergeContext(cmd.Context(), plan)
		})
		st.Done()
		undoHint(cmd, snapshot, err)
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
		mover, err := client.LibraryMover()
		if err != nil {
			return err
		}
		mover.OnProgress = st.Progress
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid manga id %q", args[0])
		}
		manga, ok, err := mover.Library.MangaByID(id)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("no manga %d in the library", id)
		}
		st.Set("loading " + args[1])
		c, warnings, err := client.AcquireByID(ctx, args[1])
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
			st.Set(fmt.Sprintf("searching %q on %s", query, c.Info().Name))
		}
		to, err := selector.Find(ctx, client.Searcher(c), "", query, libraryMoveArgs.Selector)
		if err != nil {
			return err
		}
		st.Set("planning the move")
		plan, err := mover.PlanMove(ctx, manga, c.Info(), to, library.MoveOptions{Full: libraryMoveArgs.Full})
		st.Done()
		if err != nil {
			return err
		}
		for _, line := range plan.Lines() {
			cmd.Println(line)
		}
		if !libraryMoveArgs.Apply {
			cmd.Println("run with --apply to move")
			return nil
		}
		snapshot, err := client.Operation("move", fmt.Sprintf("move %d %q to %s %s", plan.Manga.ID, plan.Manga.Title, plan.Provider.ID, plan.To.ID), func(j library.Journal) error {
			mover.Journal = j
			return mover.ApplyMove(plan)
		})
		undoHint(cmd, snapshot, err)
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
		root := path.Downloads()
		batches, err := library.Trash(afs.Afero, root)
		if err != nil {
			return err
		}
		if len(batches) == 0 {
			cmd.Println("the trash is empty")
			return nil
		}
		files := 0
		for _, b := range batches {
			files += b.Files
		}
		undoer, err := client.LibraryUndoer()
		if err != nil {
			return err
		}
		blocked, err := undoer.NeedTrash(root)
		if err != nil {
			return err
		}
		if len(blocked) > 0 {
			cmd.Printf("these operations can't be undone after it:\n")
			for _, op := range blocked {
				cmd.Printf("  %d  %s  %s  %s\n", op.ID, op.Started.Format(time.DateTime), op.Kind, op.Summary)
			}
		}
		if !libraryTrashEmptyArgs.Yes && !confirm(cmd, fmt.Sprintf("Delete the %d files in the trash?", files)) {
			return nil
		}
		snapshot, err := client.Snapshot("empty-trash")
		if err != nil {
			return err
		}
		cmd.PrintErrf("database snapshot: %s\n", snapshot)
		if err := library.EmptyTrash(afs.Afero, root); err != nil {
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

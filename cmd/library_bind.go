package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/cobra"
)

var libraryBindArgs = struct {
	Anilist, Kitsu, MyAnimeList string
	Remove                      []string
	Rename, Watches             bool
	Backups, Yes, Apply         bool
	Dismiss                     bool
}{}

func init() {
	libraryCmd.AddCommand(libraryBindCmd)
	f := libraryBindCmd.Flags()
	f.StringVar(&libraryBindArgs.Anilist, "anilist", "", "Link the AniList manga with this id")
	f.StringVar(&libraryBindArgs.Kitsu, "kitsu", "", "Link the Kitsu manga with this id")
	f.StringVar(&libraryBindArgs.MyAnimeList, "mal", "", "Link the MyAnimeList manga with this id")
	f.StringSliceVar(&libraryBindArgs.Remove, "remove", nil, "Remove the links of these sources (anilist|kitsu|mal)")
	f.BoolVar(&libraryBindArgs.Rename, "rename", false, "Also rename the folder to the name the template gives with the links")
	f.BoolVar(&libraryBindArgs.Watches, "watches", false, "Also bind the manga's watches (of other provider mangas) to the links")
	f.BoolVar(&libraryBindArgs.Backups, "backups", false, "Fill the missing Kitsu and MyAnimeList links from AniList (all mangas without an id)")
	f.BoolVarP(&libraryBindArgs.Yes, "yes", "y", false, "With --backups --apply, set them without asking (ids that differ are still only replaced when asked)")
	f.BoolVar(&libraryBindArgs.Apply, "apply", false, "Link (rename, bind, fill), instead of only listing what would happen")
	f.BoolVar(&libraryBindArgs.Dismiss, "dismiss", false, "Link neither of the Kitsu ids to pick from (not asked again until others are found)")
}

var libraryBindCmd = &cobra.Command{
	Use:   "bind [library-id]",
	Short: "Link a library manga to its AniList, Kitsu or MyAnimeList manga",
	Long: `Link a library manga (id from "mangal library ls") to metadata sources, also one without
a provider source (a folder adopted by a scan). A link replaces the source's old id
everywhere: the manga's own ids, the bindings of its provider sources and the metadata ids
of its .libmangal.json, so the old id stops matching downloads. Without flags it prints the
manga's links, where each comes from and what disagrees, and the id its folder suggests
(the "[alid-N]" tag, the .libmangal.json).

What a link implies is never done silently: the folder name the template gives with the
new AniList id (--rename does it), a watch of the manga on another provider bound to
another id (--watches binds it), another library manga with the id (merge them).

Only lists what would happen (the ids are checked) unless --apply is set.

--backups fills the Kitsu and MyAnimeList links of the mangas linked to AniList (the given
one, else all): MyAnimeList from AniList (batched), Kitsu from the links of a MangaDex
source, else from Kitsu's own AniList mappings (batched; when both exist and disagree,
neither is set and the two are saved: "mangal library bind <id>" lists them, linking any
Kitsu id settles it, --dismiss keeps neither). A link that
differs is only replaced when you say so (with --apply). Every change can be undone
("mangal library undo").`,
	Example: `  mangal library bind 12
  mangal library bind 12 --anilist 74347
  mangal library bind 12 --anilist 74347 --apply
  mangal library bind 12 --anilist 74347 --rename --watches --apply
  mangal library bind 12 --remove kitsu --apply
  mangal library bind 12 --dismiss
  mangal library bind --backups --apply`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		st := newStatus(cmd)
		defer st.Done()
		ctx := st.Context(cmd.Context())
		linker, err := client.LibraryLinker()
		if err != nil {
			return err
		}
		linker.OnProgress = st.Progress
		links, err := libraryBindLinks(cmd)
		if err != nil {
			return err
		}
		if libraryBindArgs.Yes && !libraryBindArgs.Backups {
			return errors.New("--yes goes with --backups")
		}
		if len(args) == 0 {
			if !libraryBindArgs.Backups || len(links) != 0 || libraryBindArgs.Rename || libraryBindArgs.Watches {
				return errors.New("give the library id (mangal library ls), only --backups goes without one")
			}
			mangas, err := linker.Library.Mangas()
			if err != nil {
				return err
			}
			return libraryBackups(cmd, st, linker, mangas)
		}
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid manga id %q", args[0])
		}
		manga, ok, err := linker.Library.MangaByID(id)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("no manga %d in the library", id)
		}

		if libraryBindArgs.Dismiss {
			if len(links) != 0 || libraryBindArgs.Rename || libraryBindArgs.Watches || libraryBindArgs.Backups {
				return errors.New("--dismiss goes alone")
			}
			ok, err := linker.Library.DismissLinkPick(manga.ID, metadata.IDCodeKitsu)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("library manga %d has no Kitsu ids to pick from", manga.ID)
			}
			cmd.Println("dismissed, no Kitsu id linked")
			return nil
		}
		if len(links) == 0 && !libraryBindArgs.Rename && !libraryBindArgs.Watches {
			if !libraryBindArgs.Backups {
				if libraryBindArgs.Apply {
					return errors.New("nothing to apply: give links (--anilist, --kitsu, --mal, --remove), --rename, --watches or --backups")
				}
				return printLinks(cmd, linker, manga)
			}
			return libraryBackups(cmd, st, linker, []storage.Manga{manga})
		}
		for code, id := range links {
			if id == "" {
				continue
			}
			src, _ := client.MetadataSourceOf(code)
			st.Set(fmt.Sprintf("checking %s %s", src.Name, id))
			meta, err := client.LinkMetadata(ctx, code, id)
			if err != nil {
				return err
			}
			st.Print(func() {
				if meta == nil {
					cmd.Printf("%s %s (not checked: no MyAnimeList API client)\n", src.Name, id)
				} else {
					cmd.Printf("%s %s: %s\n", src.Name, id, meta)
				}
			})
		}
		st.Set("planning")
		plan, err := linker.PlanLinks(ctx, manga, links, library.PlanOptions{})
		st.Clear()
		if err != nil {
			return err
		}
		for _, line := range plan.Lines() {
			cmd.Println(line)
		}
		opts := library.LinkOptions{Rename: libraryBindArgs.Rename, Watches: libraryBindArgs.Watches}
		renames := opts.Rename && plan.Rename != nil && plan.Rename.Skip == ""
		watches := opts.Watches && len(plan.Watches) != 0
		todo := len(plan.Changes) != 0 || renames || watches
		if todo && !libraryBindArgs.Apply {
			if len(plan.Changes) != 0 {
				cmd.Println("would link")
			}
			if renames {
				cmd.Printf("would rename to %s\n", filepath.Base(plan.Rename.To))
			}
			if watches {
				cmd.Printf("would bind %d watches\n", len(plan.Watches))
			}
		}
		if todo && libraryBindArgs.Apply {
			summary := fmt.Sprintf("link %d %q", manga.ID, manga.Title)
			snapshot, err := client.Operation("link", summary, func(j library.Journal) error {
				linker.Journal = j
				return linker.ApplyLinksContext(ctx, plan, opts)
			})
			st.Done()
			undoHint(cmd, snapshot, err)
			if err != nil {
				return err
			}
			if len(plan.Changes) != 0 {
				cmd.Println("linked")
			}
			if renames {
				cmd.Printf("renamed to %s\n", filepath.Base(plan.Rename.To))
			}
			if watches {
				cmd.Printf("bound %d watches\n", len(plan.Watches))
			}
		}
		// what's left to do
		left := plan
		if renames {
			left.Rename = nil
		}
		if watches {
			left.Watches = nil
		}
		for _, line := range left.ImpliedLines(map[string]string{"rename": "--rename", "watches": "--watches"}) {
			cmd.Println(line)
		}
		if todo && !libraryBindArgs.Apply {
			cmd.Println("run with --apply to do it")
		}
		if libraryBindArgs.Backups {
			if manga, ok, err = linker.Library.MangaByID(manga.ID); err != nil || !ok {
				return err
			}
			return libraryBackups(cmd, st, linker, []storage.Manga{manga})
		}
		return nil
	},
}

// libraryBindLinks are the links of the flags, "" for those to remove.
func libraryBindLinks(cmd *cobra.Command) (library.Links, error) {
	links := library.Links{}
	for _, s := range []struct {
		code metadata.IDCode
		flag string
		id   string
	}{
		{metadata.IDCodeAnilist, "anilist", libraryBindArgs.Anilist},
		{metadata.IDCodeKitsu, "kitsu", libraryBindArgs.Kitsu},
		{metadata.IDCodeMyAnimeList, "mal", libraryBindArgs.MyAnimeList},
	} {
		if !cmd.Flags().Changed(s.flag) {
			continue
		}
		id := strings.TrimSpace(s.id)
		if n, err := strconv.Atoi(id); err != nil || n <= 0 {
			return nil, fmt.Errorf("--%s %q isn't a positive integer", s.flag, s.id)
		}
		links[s.code] = id
	}
	for _, r := range libraryBindArgs.Remove {
		src, err := client.ParseMetadataSource(r)
		if err != nil {
			return nil, err
		}
		if _, ok := links[src.Code]; ok {
			return nil, fmt.Errorf("%s is both linked and removed", src.Name)
		}
		links[src.Code] = ""
	}
	return links, nil
}

// printLinks prints the manga's links, where they come from, and what its folder suggests.
func printLinks(cmd *cobra.Command, linker library.Linker, manga storage.Manga) error {
	info, err := linker.LinkInfo(manga)
	if err != nil {
		return err
	}
	cmd.Printf("library manga %d %q (%s)\n", manga.ID, manga.Title, manga.Path())
	for _, s := range info.Sources {
		src, _ := client.MetadataSourceOf(s.Code)
		switch {
		case s.Linked():
			line := fmt.Sprintf("  %s: %s (%s)", src.Name, s.Current(), s.Where())
			if sug := s.Suggested(); sug != "" {
				line += fmt.Sprintf(", the folder says %s: --%s %s", sug, src.Key, sug)
			}
			cmd.Println(line)
		case s.Suggested() != "":
			cmd.Printf("  %s: not linked, the folder suggests %s (%s): --%s %s\n", src.Name, s.Suggested(), s.Where(), src.Key, s.Suggested())
		default:
			cmd.Printf("  %s: not linked\n", src.Name)
		}
		if pick, ok, err := linker.Library.LinkPick(manga.ID, s.Code); err != nil {
			return err
		} else if ok && !pick.Dismissed {
			cmd.Printf("    %s\n", pickLine(pick, src.Key))
		}
	}
	if info.Marker == nil && manga.Dir != "" {
		cmd.Printf("  no %s (mangal library scan --mark writes one)\n", libmangal.FilenameMangaDirInfo)
	}
	return nil
}

// pickLine is "pick one (found 2026-10-12, AniList 3): 30 (mango-mangadex): --kitsu 30, 31
// (Kitsu mapping): --kitsu 31; or --dismiss".
func pickLine(pick storage.LinkPick, flag string) string {
	parts := make([]string, len(pick.Candidates))
	for i, c := range pick.Candidates {
		parts[i] = fmt.Sprintf("%s (%s): --%s %s", c.ID, c.FromName(), flag, c.ID)
	}
	return fmt.Sprintf("pick one (found %s for AniList %s): %s; or --dismiss", pick.Found.Format("2006-01-02"), pick.AniList, strings.Join(parts, ", "))
}

// libraryBackups fills the Kitsu and MyAnimeList links of the mangas from AniList, asking
// first (unless --yes) and for each id that differs.
func libraryBackups(cmd *cobra.Command, st *status, linker library.Linker, mangas []storage.Manga) error {
	ctx := st.Context(cmd.Context())
	finder, err := client.LibraryBackups()
	if err != nil {
		return err
	}
	finder.OnProgress = st.Progress
	backups, err := finder.Find(ctx, mangas)
	st.Clear()
	if err != nil {
		return err
	}
	_, conflicts, _, _ := library.BackupSummary(backups)
	for _, b := range backups {
		for _, line := range b.Lines() {
			cmd.Println("  " + line)
		}
	}
	replace := map[int64]bool{}
	if conflicts != 0 && libraryBindArgs.Apply {
		for _, b := range backups {
			for _, c := range b.Conflicts {
				src, _ := client.MetadataSourceOf(c.Code)
				if confirm(cmd, fmt.Sprintf("Replace the %s link of %q (%s) with AniList's %s?", src.Name, b.Manga.Title, c.Old[0], c.New)) {
					replace[b.Manga.ID] = true
				}
			}
		}
	}
	todo := 0
	for _, b := range backups {
		if len(b.Links(replace[b.Manga.ID])) != 0 {
			todo++
		}
	}
	switch {
	case todo == 0:
	case !libraryBindArgs.Apply:
		cmd.Printf("would set the links of %d mangas\n", todo)
	case libraryBindArgs.Yes || confirm(cmd, fmt.Sprintf("Set the links of %d mangas?", todo)):
		snapshot, err := client.Operation("link", fmt.Sprintf("backup links of %d mangas", todo), func(j library.Journal) error {
			linker.Journal = j
			for i, b := range backups {
				links := b.Links(replace[b.Manga.ID])
				if len(links) == 0 {
					continue
				}
				st.Progress(library.Progress{Phase: "linking", Done: i + 1, Total: len(backups), Item: b.Manga.Title})
				plan, err := linker.PlanLinks(ctx, b.Manga, links, library.PlanOptions{NoImplied: true})
				if err != nil {
					return err
				}
				if err := linker.ApplyLinksContext(ctx, plan, library.LinkOptions{}); err != nil {
					return err
				}
			}
			return nil
		})
		st.Done()
		undoHint(cmd, snapshot, err)
		if err != nil {
			return err
		}
		cmd.Printf("set the links of %d mangas\n", todo)
	default:
		cmd.Println("nothing set")
	}
	cmd.Println(finder.Summary(backups))
	if todo != 0 && !libraryBindArgs.Apply {
		cmd.Println("run with --apply to set them")
	}
	return nil
}

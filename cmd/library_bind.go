package cmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/core"
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
		ctx := changeCtx(cmd, st)
		c, err := newCore()
		if err != nil {
			return err
		}
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
			return libraryBackups(cmd, st, c, nil)
		}
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid manga id %q", args[0])
		}
		manga, err := c.Library.Manga(ctx, id)
		if err != nil {
			return err
		}

		if libraryBindArgs.Dismiss {
			if len(links) != 0 || libraryBindArgs.Rename || libraryBindArgs.Watches || libraryBindArgs.Backups {
				return errors.New("--dismiss goes alone")
			}
			plan, err := c.Changes.Plan(ctx, core.DismissPickReq{Manga: manga.ID, Source: metadata.IDCodeKitsu})
			if err != nil {
				return err
			}
			if plan.Nothing {
				return errors.New(plan.Lines[0])
			}
			if _, err := c.Changes.ApplyPlan(ctx, plan, applyOpts(st)); err != nil {
				return err
			}
			cmd.Println("dismissed, no Kitsu id linked")
			return nil
		}
		if len(links) == 0 && !libraryBindArgs.Rename && !libraryBindArgs.Watches {
			if !libraryBindArgs.Backups {
				if libraryBindArgs.Apply {
					return errors.New("nothing to apply: give links (--anilist, --kitsu, --mal, --remove), --rename, --watches or --backups")
				}
				return printLinks(ctx, cmd, c, manga)
			}
			return libraryBackups(cmd, st, c, []int64{manga.ID})
		}
		plan, err := c.Changes.Plan(ctx, core.LinkReq{Manga: manga.ID, Links: links, Rename: libraryBindArgs.Rename, Watches: libraryBindArgs.Watches})
		st.Clear()
		if err != nil {
			return err
		}
		detail := plan.Detail.(core.LinkDetail)
		for _, checked := range detail.Checked {
			src, _ := client.MetadataSourceOf(checked.Code)
			if checked.Meta == nil {
				cmd.Printf("%s %s (not checked: no MyAnimeList API client)\n", src.Name, checked.ID)
			} else {
				cmd.Printf("%s %s: %s\n", src.Name, checked.ID, checked.Meta)
			}
		}
		lp := detail.Plan
		printLines(cmd, lp.Lines())
		renames, watches := detail.Renames, detail.BindsWatches
		todo := !plan.Nothing
		if todo && !libraryBindArgs.Apply {
			if len(lp.Changes) != 0 {
				cmd.Println("would link")
			}
			if renames {
				cmd.Printf("would rename to %s\n", filepath.Base(lp.Rename.To))
			}
			if watches {
				cmd.Printf("would bind %d watches\n", len(lp.Watches))
			}
		}
		if todo && libraryBindArgs.Apply {
			res, err := c.Changes.ApplyPlan(ctx, plan, applyOpts(st))
			st.Done()
			undoHint(cmd, res.Snapshot, err)
			if err != nil {
				return err
			}
			if len(lp.Changes) != 0 {
				cmd.Println("linked")
			}
			if renames {
				cmd.Printf("renamed to %s\n", filepath.Base(lp.Rename.To))
			}
			if watches {
				cmd.Printf("bound %d watches\n", len(lp.Watches))
			}
		}
		// what's left to do
		left := lp
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
			return libraryBackups(cmd, st, c, []int64{manga.ID})
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
func printLinks(ctx context.Context, cmd *cobra.Command, c *core.Core, manga storage.Manga) error {
	info, err := c.Library.Links(ctx, manga.ID)
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
		if pick, ok, err := c.Library.LinkPick(ctx, manga.ID, s.Code); err != nil {
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

// libraryBackups fills the Kitsu and MyAnimeList links of the mangas (nil: all) from
// AniList, asking first (unless --yes) and for each id that differs.
func libraryBackups(cmd *cobra.Command, st *status, c *core.Core, mangas []int64) error {
	ctx := changeCtx(cmd, st)
	req := core.BackupsReq{Mangas: mangas}
	plan, err := c.Changes.Plan(ctx, req)
	st.Clear()
	if err != nil {
		return err
	}
	found := plan.Detail.(core.BackupsDetail)
	for _, b := range found.Backups {
		for _, line := range b.Lines() {
			cmd.Println("  " + line)
		}
	}
	if found.Conflicts != 0 && libraryBindArgs.Apply {
		for _, b := range found.Backups {
			for _, conflict := range b.Conflicts {
				src, _ := client.MetadataSourceOf(conflict.Code)
				if confirm(cmd, fmt.Sprintf("Replace the %s link of %q (%s) with AniList's %s?", src.Name, b.Manga.Title, conflict.Old[0], conflict.New)) && !slices.Contains(req.Replace, b.Manga.ID) {
					req.Replace = append(req.Replace, b.Manga.ID)
				}
			}
		}
		if len(req.Replace) != 0 {
			// the links shown, those confirmed replaced
			if plan, err = c.Changes.Variant(ctx, plan, req); err != nil {
				return err
			}
			found = plan.Detail.(core.BackupsDetail)
		}
	}
	todo := found.ToSet
	switch {
	case todo == 0:
	case !libraryBindArgs.Apply:
		cmd.Printf("would set the links of %d mangas\n", todo)
	case libraryBindArgs.Yes || confirm(cmd, fmt.Sprintf("Set the links of %d mangas?", todo)):
		res, err := c.Changes.ApplyPlan(ctx, plan, applyOpts(st))
		st.Done()
		undoHint(cmd, res.Snapshot, err)
		if err != nil {
			return err
		}
		cmd.Printf("set the links of %d mangas\n", todo)
	default:
		cmd.Println("nothing set")
	}
	cmd.Println(found.Summary)
	if todo != 0 && !libraryBindArgs.Apply {
		cmd.Println("run with --apply to set them")
	}
	return nil
}

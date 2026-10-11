package cmd

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/template"
	"github.com/luevano/mangal/v5/util/afs"
	"github.com/luevano/mangal/v5/util/cache"
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
	Apply bool
}{}

func init() {
	libraryCmd.AddCommand(libraryRenameCmd)
	libraryRenameCmd.Flags().BoolVar(&libraryRenameArgs.Apply, "apply", false, "Rename, instead of only listing the renames")
}

var libraryRenameCmd = &cobra.Command{
	Use:   "rename",
	Short: "Rename manga and provider directories to the current name templates",
	Long: `Rename the manga directories (those with a .libmangal.json, written on download)
and the provider directories in the download path to the names the current name
templates give them. Only lists the renames unless --apply is set.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		loaders, err := manager.Loaders()
		if err != nil {
			return err
		}
		namer := library.Namer{
			FS:           afs.Afero,
			Loaders:      loaders,
			ProviderName: template.Provider,
			MangaName:    template.Manga,
			Metadata:     anilist.MetadataByIDs,
		}

		dir := path.DownloadsDir()
		renames, err := namer.Plan(cmd.Context(), dir, config.Download.Provider.CreateDir.Get())
		if err != nil {
			return err
		}
		if len(renames) == 0 {
			cmd.Println("nothing to rename")
			return nil
		}

		rel := func(p string) string {
			if r, err := filepath.Rel(dir, p); err == nil {
				return r
			}
			return p
		}
		if !libraryRenameArgs.Apply {
			for _, r := range renames {
				cmd.Printf("%s -> %s\n", rel(r.From), rel(r.To))
			}
			cmd.Println("run with --apply to rename")
			return nil
		}
		_, dataDB, err := cache.DBs()
		if err != nil {
			return err
		}
		index := storage.Library{DB: dataDB}
		for i, err := range library.Apply(afs.Afero, renames) {
			r := renames[i]
			if err != nil {
				cmd.PrintErrf("skipped %s: %s\n", rel(r.From), err)
				continue
			}
			if err := index.Moved(r.From, r.To); err != nil {
				return err
			}
			cmd.Printf("%s -> %s\n", rel(r.From), rel(r.To))
		}
		return nil
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
	ValidArgsFunction: completionProviderIDs,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, dataDB, err := cache.DBs()
		if err != nil {
			return err
		}
		provider, mangaID := "", ""
		if len(args) > 0 {
			provider = args[0]
		}
		if len(args) > 1 {
			mangaID = args[1]
		}
		downloads, err := storage.Downloads{DB: dataDB}.List(provider, mangaID)
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
	libraryCmd.AddCommand(libraryMergeCmd)
	libraryMergeCmd.Flags().BoolVar(&libraryMergeArgs.Apply, "apply", false, "Merge, instead of only listing what would happen")
	libraryCmd.AddCommand(libraryTrashCmd)
	libraryTrashCmd.AddCommand(libraryTrashEmptyCmd)
}

// libraryOf opens the library of the data database.
func libraryOf() (storage.Library, error) {
	_, dataDB, err := cache.DBs()
	return storage.Library{DB: dataDB}, err
}

var libraryLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List the mangas in the library",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		library, err := libraryOf()
		if err != nil {
			return err
		}
		mangas, err := library.Mangas()
		if err != nil {
			return err
		}
		if len(mangas) == 0 {
			cmd.Println(`the library is empty, "mangal library scan" adds the mangas already downloaded`)
		}
		for _, m := range mangas {
			var sources []string
			for _, src := range m.Sources {
				sources = append(sources, src.Provider)
			}
			line := fmt.Sprintf("%d  %s  %d chapters  %s", m.ID, m.Title, m.Chapters, m.Path())
			if len(sources) != 0 {
				line += "  [" + strings.Join(sources, ", ") + "]"
			}
			cmd.Println(line)
		}
		printTrash(cmd)
		return nil
	},
}

// printTrash tells about files waiting in the trash.
func printTrash(cmd *cobra.Command) {
	batches, err := library.Trash(afs.Afero, path.DownloadsDir())
	if err != nil || len(batches) == 0 {
		return
	}
	var files int
	var size int64
	for _, b := range batches {
		files += b.Files
		size += b.Size
	}
	cmd.Printf("\n%d files (%s) in the trash, \"mangal library trash empty\" deletes them\n", files, humanSize(size))
}

var libraryScanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Check the library against the files",
	Long: `Check the library against the downloaded files: moved manga folders and chapter files
are found again, deleted ones forgotten, files nobody recorded (e.g. copied by hand) added,
and manga folders with a .libmangal.json that aren't in the library yet are added.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		lib, err := libraryOf()
		if err != nil {
			return err
		}
		root := path.DownloadsDir()
		providerDirs := config.Download.Provider.CreateDir.Get()
		scanner := library.Scanner{FS: afs.Afero, Library: lib}
		res, err := scanner.Scan(root, providerDirs)
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
			if res, err = scanner.Scan(root, providerDirs); err != nil {
				return err
			}
		}

		cmd.Printf("added %d, moved %d, removed %d chapters; found %d moved manga folders, %d new\n",
			res.Added, res.Moved, res.Removed, res.Relocated, res.New)
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
				for _, m := range res.Missing {
					if err := lib.RemoveManga(m.ID); err != nil {
						return err
					}
				}
				cmd.Println("removed")
			}
		}
		printTrash(cmd)
		return nil
	},
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
		plan, err := library.PlanMerge(afs.Afero, lib, mangas[0], mangas[1])
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
		if err := library.ApplyMerge(afs.Afero, lib, path.DownloadsDir(), plan); err != nil {
			return err
		}
		cmd.Println("merged")
		printTrash(cmd)
		return nil
	},
}

var libraryTrashCmd = &cobra.Command{
	Use:   "trash",
	Short: "List what's in the trash",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		batches, err := library.Trash(afs.Afero, path.DownloadsDir())
		if err != nil {
			return err
		}
		if len(batches) == 0 {
			cmd.Println("the trash is empty")
			return nil
		}
		for _, b := range batches {
			cmd.Printf("%s  %d files  %s  %s\n", b.Name, b.Files, humanSize(b.Size), b.Path)
		}
		return nil
	},
}

var libraryTrashEmptyCmd = &cobra.Command{
	Use:   "empty",
	Short: "Delete what's in the trash",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		root := path.DownloadsDir()
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
		if !confirm(cmd, fmt.Sprintf("Delete the %d files in the trash?", files)) {
			return nil
		}
		if err := library.EmptyTrash(afs.Afero, root); err != nil {
			return err
		}
		cmd.Println("emptied")
		return nil
	},
}

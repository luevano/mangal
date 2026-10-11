package cmd

import (
	"context"
	"path/filepath"
	"time"

	"github.com/luevano/libmangal/metadata"
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
		ani := anilist.Anilist()
		namer := library.Namer{
			FS:           afs.Afero,
			Loaders:      loaders,
			ProviderName: template.Provider,
			MangaName:    template.Manga,
			Metadata: func(ctx context.Context, ids []metadata.ID) (metadata.Metadata, error) {
				for _, id := range ids {
					if id.Source != metadata.IDSourceAnilist {
						continue
					}
					meta, found, err := ani.SearchByIDContext(ctx, id.Value())
					if err != nil || found {
						return meta, err
					}
				}
				return nil, nil
			},
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
		index := storage.MangaDirs{DB: dataDB}
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

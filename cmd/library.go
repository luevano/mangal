package cmd

import (
	"context"
	"path/filepath"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/client/anilist"
	"github.com/luevano/mangal/config"
	"github.com/luevano/mangal/library"
	"github.com/luevano/mangal/path"
	"github.com/luevano/mangal/provider/manager"
	"github.com/luevano/mangal/template"
	"github.com/luevano/mangal/util/afs"
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
		for i, err := range library.Apply(afs.Afero, renames) {
			r := renames[i]
			if err != nil {
				cmd.PrintErrf("skipped %s: %s\n", rel(r.From), err)
				continue
			}
			cmd.Printf("%s -> %s\n", rel(r.From), rel(r.To))
		}
		return nil
	},
}

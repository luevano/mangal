package cmd

import (
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/afs"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/spf13/cobra"
)

var migrateArgs = struct {
	Apply bool
}{}

func init() {
	rootCmd.AddCommand(migrateCmd)
	migrateCmd.Flags().BoolVar(&migrateArgs.Apply, "apply", false, "Migrate, instead of only listing what would change")
}

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Move data under old provider ids to the current ones",
	Long: `Providers that were renamed keep their old ids in their history, data saved under an
old id still works but can be moved to the current id: the library index entries, the
downloads and the .libmangal.json files of the downloaded mangas get the new id, and the old cache is
dropped (it rebuilds). Folders aren't renamed or moved, "mangal library rename" does that.

Only lists what would change unless --apply is set.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		loaders, err := manager.Loaders()
		if err != nil {
			return err
		}
		cacheDB, dataDB, err := cache.DBs()
		if err != nil {
			return err
		}
		oldIDs, err := storage.FindOldIDs(cacheDB, dataDB, loaders)
		if err != nil {
			return err
		}
		markers, err := library.OldMarkers(afs.Afero, path.DownloadsDir(), config.Download.Provider.CreateDir.Get(), loaders)
		if err != nil {
			return err
		}
		if len(oldIDs) == 0 && len(markers) == 0 {
			cmd.Println("nothing to migrate")
			return nil
		}

		for _, id := range oldIDs {
			cmd.Printf("%s -> %s (%s): %d library mangas, %d downloads, %d cache entries (dropped)\n", id.Old, id.New, id.Name, id.Sources, id.Downloads, id.Cache)
		}
		for _, marker := range markers {
			cmd.Printf("%s: %s -> %s\n", marker.Dir, marker.Old, marker.New)
		}
		if !migrateArgs.Apply {
			cmd.Println("run with --apply to migrate")
			return nil
		}

		if err := storage.MigrateOldIDs(cacheDB, dataDB, oldIDs); err != nil {
			return err
		}
		for _, marker := range markers {
			if err := library.UpdateMarker(afs.Afero, marker); err != nil {
				return err
			}
		}
		cmd.Println("migrated")
		return nil
	},
}

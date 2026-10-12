package cmd

import (
	"fmt"
	"strings"

	"github.com/luevano/mangal/v5/client"
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

Watches move too, and need a re-link: the new site may use other manga ids. The scheduler
skips them until a check finds the manga ("mangal watch check <id>") or they're re-linked
("mangal watch move <id>"); the ones left are listed at the end.

Only lists what would change unless --apply is set.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		loaders, err := manager.Loaders()
		if err != nil {
			return err
		}
		st, err := cache.Storage()
		if err != nil {
			return err
		}
		root, err := path.Ensure(path.Downloads())
		if err != nil {
			return err
		}
		migration, err := library.FindMigration(afs.Afero, st, root, config.Download.Provider.CreateDir.Get(), loaders)
		if err != nil {
			return err
		}
		if migration.Empty() {
			cmd.Println("nothing to migrate")
			return nil
		}
		for _, line := range migration.Lines() {
			cmd.Println(line)
		}
		if !migrateArgs.Apply {
			cmd.Println("run with --apply to migrate")
			return nil
		}
		if err := migration.ApplyTo(afs.Afero, nil, st, client.LibraryModes()); err != nil {
			return err
		}
		cmd.Println("migrated")
		return relinkWarning(cmd, st.Watches())
	},
}

// relinkWarning lists the watches that need a re-link (e.g. moved from an old provider
// id whose site changed) and how to fix them.
func relinkWarning(cmd *cobra.Command, watches storage.Watches) error {
	all, err := watches.List()
	if err != nil {
		return err
	}
	var lines []string
	for _, w := range all {
		if w.NeedsRelink {
			lines = append(lines, fmt.Sprintf("  %d  %s (%s %s): %s", w.ID, w.Title, w.Provider, w.MangaID, w.RelinkReason))
		}
	}
	if len(lines) == 0 {
		return nil
	}
	cmd.PrintErrf("warning: %d watches need a re-link, the scheduler skips them until then:\n%s\n", len(lines), strings.Join(lines, "\n"))
	cmd.PrintErrln(`check one ("mangal watch check <id>": finding its manga clears it) or re-link it: "mangal watch move <id> [provider] [query|mid:<id>]", or e on the watch in the TUI's watches (w on the home screen)`)
	return nil
}

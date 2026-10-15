package core

import (
	"context"
	"fmt"

	"github.com/luevano/mangal/v5/library"
)

// MigrateReq moves the data under old provider ids (renamed providers) to the current
// ones: index rows, downloads, reads, watches and the .libmangal.json files; the old
// cache is dropped. Folders keep their names.
type MigrateReq struct{}

func (MigrateReq) Kind() Kind { return KindMigrate }

func (MigrateReq) plan(ctx context.Context, c *Core) (*prepared, error) {
	files, err := c.existingFiles()
	if err != nil {
		return nil, err
	}
	loaders, err := c.env.Loaders()
	if err != nil {
		return nil, err
	}
	migration, err := library.FindMigration(c.env.FS, c.env.Storage, files.Root, c.env.ProviderDirs(), loaders, progressOf(ctx))
	if err != nil {
		return nil, err
	}
	tables := append(append(append([]string{}, libraryTables...), watchTables...), readTables...)
	if migration.Empty() {
		return &prepared{plan: Plan{Lines: []string{"nothing to migrate"}, Nothing: true, Detail: migration}, tables: tables}, nil
	}
	summary := fmt.Sprintf("migrate %d old provider ids, %d markers", len(migration.OldIDs), len(migration.Markers))
	return &prepared{
		plan:   Plan{Summary: summary, Lines: migration.Lines(), Detail: migration},
		tables: tables,
		apply: func(ctx context.Context, _ ApplyOpts, res *OpResult) error {
			return c.journaled(ctx, "migrate", summary, res, func(j library.Journal) error {
				m := migration
				m.OnProgress = progressOf(ctx)
				return m.ApplyTo(c.env.FS, j, c.env.Storage, files.Modes)
			})
		},
	}, nil
}

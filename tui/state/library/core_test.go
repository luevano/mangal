package library

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/spf13/afero"
)

// useCore makes the screens' core one over the test's library, root and fs: provider
// directories named after the provider, folders after the manga's title (its metadata's
// when it has some), no AniList.
func useCore(t *testing.T, lib storage.Library, fsys afero.Fs, root string) *core.Core {
	t.Helper()
	c, err := core.New(context.Background(), core.Env{
		Storage:      storage.Storage{Data: lib.DB},
		FS:           fsys,
		Root:         func() string { return root },
		ProviderDirs: func() bool { return true },
		Modes:        func() library.Modes { return library.DefaultModes },
		BackupsDir:   filepath.Join(t.TempDir(), "backups"),
		Loaders:      func() ([]libmangal.ProviderLoader, error) { return nil, nil },
		ProviderName: func(p libmangal.ProviderInfo) string { return p.Name },
		MangaName: func(_ libmangal.ProviderInfo, m mangadata.Manga) string {
			if meta := m.Metadata(); metadata.Validate(meta) == nil {
				return meta.String()
			}
			return m.Info().Title
		},
		Metadata: func(context.Context, []metadata.ID) (metadata.Metadata, error) { return nil, nil },
		CheckLink: func(context.Context, metadata.IDCode, string) (metadata.Metadata, error) {
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	old := base.Core
	base.Core = func() (*core.Core, error) { return c, nil }
	t.Cleanup(func() { base.Core = old })
	return c
}

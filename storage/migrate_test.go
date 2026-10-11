package storage

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/luevano/libmangal"
)

type loader struct{ info libmangal.ProviderInfo }

func (l loader) String() string                                   { return l.info.Name }
func (l loader) Info() libmangal.ProviderInfo                     { return l.info }
func (l loader) Load(context.Context) (libmangal.Provider, error) { return nil, nil }

func openBoth(t *testing.T) (*DB, *DB) {
	t.Helper()
	dir := t.TempDir()
	cache, err := OpenCache(filepath.Join(dir, FilenameCache))
	if err != nil {
		t.Fatal(err)
	}
	data, err := OpenData(filepath.Join(dir, FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	return cache, data
}

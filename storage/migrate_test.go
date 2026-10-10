package storage

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
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

func TestMangaDirsAndOldIDs(t *testing.T) {
	cache, data := openBoth(t)
	weebCentral := libmangal.ProviderInfo{ID: "mango-weebcentral", Name: "Weeb Central", History: []libmangal.ProviderAlias{{ID: "mango-mangasee", Name: "MangaSee"}}}
	loaders := []libmangal.ProviderLoader{loader{weebCentral}}
	dirs := MangaDirs{DB: data}

	old := libmangal.MangaDirInfo{Provider: "mango-mangasee", Manga: mangadata.MangaInfo{ID: "berserk", Title: "Berserk"}}
	if err := dirs.RememberMangaDir("/lib", "Berserk", old); err != nil {
		t.Fatal(err)
	}
	// found with the current provider through its history
	want := libmangal.MangaDirInfo{Provider: "mango-weebcentral", Manga: mangadata.MangaInfo{ID: "berserk"}}
	if name, ok, err := dirs.FindMangaDir("/lib", weebCentral, want); err != nil || !ok || name != "Berserk" {
		t.Fatalf("got %q %t %v", name, ok, err)
	}

	_ = KV{DB: cache, Table: TableChapters, Owner: "mango-mangasee"}.Set("x", 1)
	_ = KV{DB: cache, Table: TableChapters, Owner: "mango-weebcentral"}.Set("x", 1)

	oldIDs, err := FindOldIDs(cache, data, loaders)
	if err != nil {
		t.Fatal(err)
	}
	if len(oldIDs) != 1 || oldIDs[0] != (OldID{Old: "mango-mangasee", New: "mango-weebcentral", Name: "Weeb Central", MangaDirs: 1, Cache: 1}) {
		t.Fatalf("got %+v", oldIDs)
	}

	if err := MigrateOldIDs(cache, data, oldIDs); err != nil {
		t.Fatal(err)
	}
	if oldIDs, _ := FindOldIDs(cache, data, loaders); len(oldIDs) != 0 {
		t.Errorf("still old ids after migrating: %+v", oldIDs)
	}
	var provider string
	if err := data.QueryRow(`SELECT provider FROM manga_dirs`).Scan(&provider); err != nil || provider != "mango-weebcentral" {
		t.Errorf("provider %q, %v", provider, err)
	}
	if name, ok, _ := dirs.FindMangaDir("/lib", weebCentral, want); !ok || name != "Berserk" {
		t.Errorf("not found after migrating: %q", name)
	}
	var n int
	_ = cache.QueryRow(`SELECT count(*) FROM chapters`).Scan(&n)
	if n != 1 {
		t.Errorf("%d cache rows, want only the current id's", n)
	}
}

func TestMangaDirsMoved(t *testing.T) {
	_, data := openBoth(t)
	dirs := MangaDirs{DB: data}
	info := libmangal.MangaDirInfo{Provider: "p", Manga: mangadata.MangaInfo{ID: "1"}}
	_ = dirs.RememberMangaDir("/lib/MangaSee", "Berserk", info)

	// the manga directory, then its provider directory
	if err := dirs.Moved("/lib/MangaSee/Berserk", "/lib/MangaSee/Berserk (1989)"); err != nil {
		t.Fatal(err)
	}
	if err := dirs.Moved("/lib/MangaSee", "/lib/Weeb Central"); err != nil {
		t.Fatal(err)
	}
	var parent, name string
	if err := data.QueryRow(`SELECT parent, name FROM manga_dirs`).Scan(&parent, &name); err != nil || parent != "/lib/Weeb Central" || name != "Berserk (1989)" {
		t.Errorf("got %q %q %v", parent, name, err)
	}
}

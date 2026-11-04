package client

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/storage"
)

func TestLibraryIndexSkipsTemp(t *testing.T) {
	db, err := storage.OpenData(filepath.Join(t.TempDir(), storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	lib := storage.Library{DB: db}
	temp := filepath.Join(t.TempDir(), "mangal-1000")
	index := libraryIndex{library: lib, temp: temp}

	info := libmangal.MangaDirInfo{Provider: "p", Manga: mangadata.MangaInfo{ID: "m", Title: "M"}}
	// a chapter read without downloading it
	if err := index.RememberMangaDir(filepath.Join(temp, "p"), "M", info); err != nil {
		t.Fatal(err)
	}
	if mangas, err := lib.Mangas(); err != nil || len(mangas) != 0 {
		t.Fatalf("temp folder in the library: %v, %v", mangas, err)
	}

	// a real download is remembered
	root := filepath.Join(t.TempDir(), "downloads")
	if err := index.RememberMangaDir(root, "M", info); err != nil {
		t.Fatal(err)
	}
	if name, found, err := index.FindMangaDir(root, libmangal.ProviderInfo{ID: "p"}, info); err != nil || !found || name != "M" {
		t.Errorf("download folder: %q, %v, %v", name, found, err)
	}
	// "mangal-1000x" isn't inside the temp dir
	if index.inTemp(temp+"x") || !index.inTemp(filepath.Join(temp, "a", "b")) {
		t.Error("inTemp")
	}
}

// a download's .libmangal.json gets the bindings' ids and the provider's reported ones
func TestLibraryIndexMangaDirInfo(t *testing.T) {
	db, err := storage.OpenData(filepath.Join(t.TempDir(), storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	lib := storage.Library{DB: db}
	index := libraryIndex{library: lib, temp: t.TempDir()}
	if err := (storage.Bindings{DB: db}).Set(storage.Binding{Provider: "p", MangaID: "m", Source: metadata.IDCodeKitsu, ID: "5"}); err != nil {
		t.Fatal(err)
	}
	mu := metadata.ID{Raw: "9", Source: metadata.IDSourceMangaUpdates, Code: metadata.IDCodeMangaUpdates}
	reported.Store(reportedKey("p", "m"), []metadata.ID{mu})
	t.Cleanup(func() { reported.Delete(reportedKey("p", "m")) })

	al := metadata.ID{Raw: "1", Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}
	info := libmangal.MangaDirInfo{Provider: "p", Manga: mangadata.MangaInfo{ID: "m"}, MetadataIDs: []metadata.ID{al}}
	got, err := index.MangaDirInfoContext(context.Background(), t.TempDir(), "M", info)
	want := []metadata.ID{al, {Raw: "5", Source: metadata.IDSourceKitsu, Code: metadata.IDCodeKitsu}, mu}
	if err != nil || !slices.Equal(got.MetadataIDs, want) {
		t.Errorf("got %+v, %v, want %+v", got.MetadataIDs, err, want)
	}
}

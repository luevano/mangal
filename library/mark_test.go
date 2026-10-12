package library

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/spf13/afero"
)

func TestMark(t *testing.T) {
	root := t.TempDir()
	lib := newLibrary(t)
	files := Files{FS: afero.NewOsFs(), Library: lib, Root: root, Modes: DefaultModes}
	see := filepath.Join(root, "mango-mangasee")
	// a source under an old provider id, no marker
	touch(t, filepath.Join(see, "Berserk", "1.cbz"))
	record(t, lib, see, "Berserk", "1.cbz", 1, "mango-mangasee")
	al := metadata.ID{Raw: "30002", Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}
	if err := lib.RememberMangaDir(see, "Berserk", libmangal.MangaDirInfo{MetadataIDs: []metadata.ID{al}}); err != nil {
		t.Fatal(err)
	}
	// adopted from guesses: no source
	touch(t, filepath.Join(see, "Guessed", "1.cbz"))
	if err := lib.RememberMangaDir(see, "Guessed", libmangal.MangaDirInfo{Manga: mangadata.MangaInfo{Title: "Guessed"}}); err != nil {
		t.Fatal(err)
	}
	// has its marker already
	writeMarker(t, filepath.Join(see, "Vagabond"), libmangal.MangaDirInfo{Provider: "x", Manga: mangadata.MangaInfo{ID: "v"}})
	if err := lib.RememberMangaDir(see, "Vagabond", libmangal.MangaDirInfo{Provider: "mango-weebcentral", Manga: mangadata.MangaInfo{ID: "v2"}}); err != nil {
		t.Fatal(err)
	}

	weeb := libmangal.ProviderInfo{ID: "mango-weebcentral", Name: "Weeb Central", History: []libmangal.ProviderAlias{{ID: "mango-mangasee"}}}
	res, err := files.Mark([]libmangal.ProviderLoader{loader{weeb}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Marked) != 1 || len(res.Unknown) != 1 || res.Unknown[0].Dir != "Guessed" {
		t.Fatalf("result %+v", res)
	}
	info, err := libmangal.ReadMangaDirInfo(files.FS, filepath.Join(see, "Berserk"))
	if err != nil || info.Provider != "mango-weebcentral" || info.Manga.ID != "berserk" || info.Manga.Title != "Berserk" || len(info.MetadataIDs) != 1 || info.MetadataIDs[0] != al {
		t.Errorf("marker %+v, %v", info, err)
	}
	if st, _ := os.Stat(filepath.Join(see, "Berserk", libmangal.FilenameMangaDirInfo)); st.Mode().Perm() != DefaultModes.File {
		t.Errorf("mode %v", st.Mode())
	}
	if info, _ := libmangal.ReadMangaDirInfo(files.FS, filepath.Join(see, "Vagabond")); info.Provider != "x" {
		t.Errorf("replaced a marker: %+v", info)
	}
	if _, err := os.Stat(filepath.Join(see, "Guessed", libmangal.FilenameMangaDirInfo)); !os.IsNotExist(err) {
		t.Error("marked a folder without a source")
	}

	// the scan relocates it by the marker, rename knows the provider directory by it
	if err := os.Rename(filepath.Join(see, "Berserk"), filepath.Join(see, "Berserk (1989)")); err != nil {
		t.Fatal(err)
	}
	scan, err := Scanner{FS: files.FS, Library: lib}.Scan(root, true)
	if err != nil || scan.Relocated != 1 {
		t.Errorf("scan %+v, %v", scan, err)
	}
	if res, _ := files.Mark(nil); len(res.Marked) != 0 {
		t.Errorf("marked twice: %+v", res.Marked)
	}
}

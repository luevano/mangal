package storage

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
)

type fakeChapter struct{ number float32 }

func (c fakeChapter) String() string { return "" }
func (c fakeChapter) Info() mangadata.ChapterInfo {
	return mangadata.ChapterInfo{Number: c.number}
}
func (c fakeChapter) Volume() mangadata.Volume { return nil }

var weebCentral = libmangal.ProviderInfo{ID: "mango-weebcentral", Name: "Weeb Central", History: []libmangal.ProviderAlias{{ID: "mango-mangasee", Name: "MangaSee"}}}

func TestMigrateLibrary(t *testing.T) {
	path := filepath.Join(t.TempDir(), FilenameData)
	// a data database from mangal 5.13.2 (schema 2)
	old, err := Open(path, dataMigrations[:2])
	if err != nil {
		t.Fatal(err)
	}
	info, _ := json.Marshal(libmangal.MangaDirInfo{Provider: "mango-mangasee", Manga: mangadata.MangaInfo{ID: "berserk", Title: "Berserk"},
		MetadataIDs: []metadata.ID{{Raw: "30002", Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}}})
	if _, err := old.Exec(`INSERT INTO manga_dirs VALUES ('/lib', 'Berserk', 'mango-mangasee', 'berserk', ?, 0)`, info); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`INSERT INTO downloads VALUES ('/lib/Berserk/[0001.0] Ch 1.cbz', 'mango-mangasee', 'berserk', 'Berserk', 1, 1, 'Ch 1', 'u', 'CBZ', 20, 0)`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	data, err := OpenData(path)
	if err != nil {
		t.Fatal(err)
	}
	library := Library{DB: data}
	mangas, err := library.Mangas()
	if err != nil {
		t.Fatal(err)
	}
	if len(mangas) != 1 || mangas[0].Path() != "/lib/Berserk" || len(mangas[0].Sources) != 1 || len(mangas[0].MetadataIDs) != 1 || mangas[0].Chapters != 1 {
		t.Fatalf("got %+v", mangas)
	}
	path, provider, found, err := library.FindChapter("/lib/Berserk", fakeChapter{1}, libmangal.FormatCBZ)
	if err != nil || !found || provider != "mango-mangasee" || path != "/lib/Berserk/[0001.0] Ch 1.cbz" {
		t.Errorf("got %q %q %t %v", path, provider, found, err)
	}
}

func TestLibrary(t *testing.T) {
	cache, data := openBoth(t)
	library := Library{DB: data}

	// a folder downloaded from the old id, found with the current one
	berserk := libmangal.MangaDirInfo{Provider: "mango-mangasee", Manga: mangadata.MangaInfo{ID: "berserk", Title: "Berserk"}}
	if err := library.RememberMangaDir("/lib", "Berserk", berserk); err != nil {
		t.Fatal(err)
	}
	want := libmangal.MangaDirInfo{Provider: "mango-weebcentral", Manga: mangadata.MangaInfo{ID: "berserk"}}
	if name, ok, err := library.FindMangaDir("/lib", weebCentral, want); err != nil || !ok || name != "Berserk" {
		t.Fatalf("got %q %t %v", name, ok, err)
	}

	// chapter 1 from two providers into the same folder: one chapter, found by number
	record := func(provider, file, title string, number float32, format string) {
		t.Helper()
		err := library.RecordDownload("/lib", "Berserk", Download{
			Path: filepath.Join("/lib/Berserk", file), Provider: provider, MangaID: "berserk", MangaTitle: "Berserk",
			Volume: 1, Chapter: number, ChapterTitle: title, Format: format, Pages: 10, Downloaded: time.Unix(1, 0),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	record("mango-mangasee", "[0001.0] The Black Swordsman.cbz", "The Black Swordsman", 1, "CBZ")
	record("mango-mangadex", "Ch. 1.pdf", "", 1, "PDF")
	record("mango-mangasee", "[0002.0] The Brand.cbz", "The Brand", 2, "CBZ")

	if path, provider, found, _ := library.FindChapter("/lib/Berserk", fakeChapter{1}, libmangal.FormatCBZ); !found || provider != "mango-mangasee" || filepath.Base(path) != "[0001.0] The Black Swordsman.cbz" {
		t.Errorf("cbz: %q %q %t", path, provider, found)
	}
	if _, _, found, _ := library.FindChapter("/lib/Berserk", fakeChapter{3}, libmangal.FormatCBZ); found {
		t.Error("found chapter 3")
	}

	mangas, _ := library.Mangas()
	if len(mangas) != 1 || mangas[0].Chapters != 2 || len(mangas[0].Sources) != 2 {
		t.Fatalf("got %+v", mangas)
	}
	var name string
	if err := data.QueryRow(`SELECT name FROM library_chapters WHERE number = 1`).Scan(&name); err != nil || name != "The Black Swordsman" {
		t.Errorf("chapter 1 name %q (an empty title doesn't replace it), %v", name, err)
	}
	if downloads, _ := library.Downloads(mangas[0].ID); len(downloads) != 3 {
		t.Errorf("%d downloads, want 3", len(downloads))
	}

	// renamed folders keep the library in sync
	if err := library.Moved("/lib/Berserk", "/lib/Berserk (1989)"); err != nil {
		t.Fatal(err)
	}
	if _, _, found, _ := library.FindChapter("/lib/Berserk (1989)", fakeChapter{2}, libmangal.FormatCBZ); !found {
		t.Error("chapter 2 not found after the move")
	}

	// old ids
	oldIDs, err := FindOldIDs(cache, data, []libmangal.ProviderLoader{loader{weebCentral}})
	if err != nil || len(oldIDs) != 1 || oldIDs[0].Sources != 1 || oldIDs[0].Downloads != 2 {
		t.Fatalf("got %+v %v", oldIDs, err)
	}
	if err := MigrateOldIDs(cache, data, oldIDs); err != nil {
		t.Fatal(err)
	}
	if oldIDs, _ := FindOldIDs(cache, data, []libmangal.ProviderLoader{loader{weebCentral}}); len(oldIDs) != 0 {
		t.Errorf("still %+v", oldIDs)
	}
}

func openData(t *testing.T) *DB {
	t.Helper()
	_, data := openBoth(t)
	return data
}

func TestMovedNonASCII(t *testing.T) {
	library := Library{DB: openData(t)}
	for _, dir := range []string{"Café", "ベルセルク"} {
		file := filepath.Join("/lib", dir, "[0001.0] Ch 1.cbz")
		if err := library.RecordDownload("/lib", dir, Download{Path: file, Provider: "p", MangaID: dir, MangaTitle: dir, Chapter: 1, Format: "CBZ"}); err != nil {
			t.Fatal(err)
		}
		to := filepath.Join("/lib", dir+" (1989)")
		if err := library.Moved(filepath.Join("/lib", dir), to); err != nil {
			t.Fatal(err)
		}
		if ok, err := library.HasDownload(filepath.Join(to, "[0001.0] Ch 1.cbz")); err != nil || !ok {
			t.Errorf("%s: download not moved (%v)", dir, err)
		}
	}
}

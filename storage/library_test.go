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

func TestMigrateOldIDs(t *testing.T) {
	cache, data := openBoth(t)
	st := Storage{Cache: cache, Data: data}
	library := st.Library()
	loaders := []libmangal.ProviderLoader{loader{weebCentral}}

	// the same manga under both ids, and one only under the old id
	for _, src := range []struct{ provider, id, dir string }{
		{"mango-mangasee", "berserk", "Berserk"},
		{"mango-weebcentral", "berserk", "Berserk"},
		{"mango-mangasee", "vagabond", "Vagabond"},
	} {
		err := library.RecordDownload("/lib", src.dir, Download{Path: filepath.Join("/lib", src.dir, src.provider+".cbz"), Provider: src.provider, MangaID: src.id, MangaTitle: src.dir, Chapter: 1, Format: "CBZ"})
		if err != nil {
			t.Fatal(err)
		}
	}
	opts := StoreOptions{TTL: time.Hour, MirrorTTL: time.Hour}
	_ = st.Store("mango-mangasee", "chapters", opts).Set("x", 1)
	_ = st.Store("mango-mangasee", "mirrors", opts).Set("mango-mangasee", "https://mangasee")
	_ = st.AccountsStore(time.Hour).Set("mango-mangasee", 1)
	_ = st.Store("mango-weebcentral", "chapters", opts).Set("x", 1)

	oldIDs, err := st.FindOldIDs(loaders)
	if err != nil || len(oldIDs) != 1 {
		t.Fatalf("got %+v, %v", oldIDs, err)
	}
	if got := oldIDs[0]; got.Old != "mango-mangasee" || got.New != "mango-weebcentral" || got.Sources != 2 || got.Downloads != 2 || got.Cache != 3 {
		t.Errorf("got %+v", got)
	}
	if err := st.MigrateOldIDs(oldIDs); err != nil {
		t.Fatal(err)
	}
	if oldIDs, err := st.FindOldIDs(loaders); err != nil || len(oldIDs) != 0 {
		t.Errorf("still %+v, %v", oldIDs, err)
	}
	var n int
	_ = data.QueryRow(`SELECT count(*) FROM library_sources WHERE provider = 'mango-weebcentral'`).Scan(&n)
	if n != 2 {
		t.Errorf("%d sources under the new id, want 2", n)
	}
	if n, _ := countProviderRows(cache, "mango-weebcentral"); n != 1 {
		t.Errorf("%d cache entries of the new id, want its own 1 kept", n)
	}
}

type fakeManga struct {
	info mangadata.MangaInfo
	meta metadata.Metadata
}

func (m fakeManga) String() string                     { return m.info.Title }
func (m fakeManga) Info() mangadata.MangaInfo          { return m.info }
func (m fakeManga) Metadata() metadata.Metadata        { return m.meta }
func (m fakeManga) SetMetadata(meta metadata.Metadata) {}

type fakeMeta struct {
	metadata.Metadata
	id metadata.ID
}

func (m fakeMeta) ID() metadata.ID         { return m.id }
func (m fakeMeta) ExtraIDs() []metadata.ID { return nil }

func TestMangaFor(t *testing.T) {
	library := Library{DB: openData(t)}
	anilistID := metadata.ID{Raw: "30002", Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}
	// downloaded under the old id
	_ = library.RememberMangaDir("/lib/MangaSee", "Berserk", libmangal.MangaDirInfo{Provider: "mango-mangasee", Manga: mangadata.MangaInfo{ID: "berserk", Title: "Berserk"}})
	// found by a scan, no source yet
	_ = library.RememberMangaDir("/lib/Weeb Central", "Vagabond", libmangal.MangaDirInfo{Manga: mangadata.MangaInfo{Title: "Vagabond"}, MetadataIDs: []metadata.ID{anilistID}})

	if m, ok, err := library.MangaFor(weebCentral, fakeManga{info: mangadata.MangaInfo{ID: "berserk"}}, true); err != nil || !ok || m.Dir != "Berserk" {
		t.Errorf("by source: %+v, %t, %v", m, ok, err)
	}
	vagabond := fakeManga{info: mangadata.MangaInfo{ID: "vagabond"}, meta: fakeMeta{id: anilistID}}
	if m, ok, err := library.MangaFor(weebCentral, vagabond, true); err != nil || !ok || m.Dir != "Vagabond" {
		t.Errorf("by metadata: %+v, %t, %v", m, ok, err)
	}
	other := libmangal.ProviderInfo{ID: "mango-mangadex", Name: "MangaDex"}
	if _, ok, _ := library.MangaFor(other, vagabond, true); ok {
		t.Error("found in another provider's directory")
	}
	if _, ok, _ := library.MangaFor(other, vagabond, false); !ok {
		t.Error("not found without provider directories")
	}
}

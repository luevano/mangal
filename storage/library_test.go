package storage

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
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

	// by a binding's id: elsewhere with provider directories, linked without
	ids := BindingIDs([]Binding{{Source: "al", ID: "30002"}, {Source: "xx", ID: "1"}})
	if len(ids) != 1 || ids[0] != anilistID {
		t.Fatalf("binding ids %+v", ids)
	}
	if match, err := library.Match(other, "v", ids, true); err != nil || match.Linked || !match.Elsewhere || match.Manga.Dir != "Vagabond" {
		t.Errorf("elsewhere: %+v, %v", match, err)
	}
	if match, _ := library.Match(other, "v", ids, false); !match.Linked || match.Elsewhere {
		t.Errorf("no provider dirs: %+v", match)
	}
	all, _ := library.Mangas()
	if match := MatchLibrary(all, other, "v", ids, true); !match.Elsewhere || match.Manga.Dir != "Vagabond" {
		t.Errorf("list elsewhere: %+v", match)
	}
	if match := MatchLibrary(all, weebCentral, "berserk", nil, true); !match.Linked || match.Manga.Dir != "Berserk" {
		t.Errorf("list by old source: %+v", match)
	}
	if match := MatchLibrary(all, other, "x", nil, true); match.Linked || match.Elsewhere {
		t.Errorf("list nothing: %+v", match)
	}
}

func TestLibraryMove(t *testing.T) {
	data := openData(t)
	library, watches, bindings := Library{DB: data}, Watches{DB: data}, Bindings{DB: data}
	file := "/lib/Asura/Berserk/[0001.0] Ch 1.cbz"
	if err := library.RecordDownload("/lib/Asura", "Berserk", Download{Path: file, Provider: "asura", MangaID: "b", MangaTitle: "Berserk", Chapter: 1, Format: "CBZ"}); err != nil {
		t.Fatal(err)
	}
	if err := library.RecordDownload("/lib/Asura", "Vagabond", Download{Path: "/lib/Asura/Vagabond/1.cbz", Provider: "asura", MangaID: "v", MangaTitle: "Vagabond", Chapter: 1, Format: "CBZ"}); err != nil {
		t.Fatal(err)
	}
	m, _, _ := library.MangaBySource("asura", "b")
	vagabond, _, _ := library.MangaBySource("asura", "v")
	w, _ := watches.Add(Watch{Provider: "asura", MangaID: "b", Title: "Berserk", LibraryManga: m.ID, RelinkReason: "gone"})
	_ = bindings.Set(Binding{Provider: "asura", MangaID: "b", Source: "al", ID: "30002"})
	_ = bindings.Set(Binding{Provider: "asura", MangaID: "b", Source: "kt", ID: "1"})
	_ = bindings.Set(Binding{Provider: "pill", MangaID: "1/berserk", Source: "kt", ID: "2"})

	move := MangaMove{
		Manga: m.ID, Source: Source{Provider: "asura", MangaID: "v", Title: "Vagabond"},
		From: m.Path(), To: "/lib/Pill/Berserk", Watch: w.ID, At: time.Unix(1_000_000, 0),
	}
	if err := library.Move(move); err == nil {
		t.Error("moved onto another library manga's source")
	}
	if got, _, _ := library.MangaByID(vagabond.ID); len(got.Sources) != 1 {
		t.Errorf("vagabond %+v", got)
	}

	move.Source = Source{Provider: "pill", MangaID: "1/berserk", Title: "Berserk (pill)"}
	move.Bindings = []Binding{{Source: "al", ID: "30002"}, {Source: "kt", ID: "1"}}
	move.Title = "Berserk (pill)"
	if err := library.Move(move); err != nil {
		t.Fatal(err)
	}
	got, _, _ := library.MangaByID(m.ID)
	if got.Path() != "/lib/Pill/Berserk" || len(got.Sources) != 2 || got.Title != "Berserk (pill)" ||
		got.Sources[0].Provider != "pill" || !got.Sources[0].Current || got.Sources[0].Moved.Unix() != 1_000_000 || got.Sources[1].Current {
		t.Errorf("manga %+v", got)
	}
	if ok, _ := library.HasDownload("/lib/Pill/Berserk/[0001.0] Ch 1.cbz"); !ok {
		t.Error("the download didn't move")
	}
	// a later download from the old source makes it the current one again
	if err := library.RecordDownload("/lib/Pill", "Berserk", Download{Path: "/lib/Pill/Berserk/2.cbz", Provider: "asura", MangaID: "b", MangaTitle: "Berserk", Chapter: 2, Format: "CBZ", Downloaded: time.Unix(2_000_000, 0)}); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := library.MangaByID(m.ID); got.Sources[0].Provider != "asura" || !got.Sources[0].Current || got.Sources[1].Current {
		t.Errorf("after a download %+v", got.Sources)
	}
	gotW, _ := watches.Get(w.ID)
	if gotW.Provider != "pill" || gotW.MangaID != "1/berserk" || gotW.Title != "Berserk (pill)" || gotW.NeedsRelink || gotW.LibraryManga != m.ID {
		t.Errorf("watch %+v", gotW)
	}
	// the new manga's own Kitsu binding stays, the old manga keeps its bindings
	if list, _ := bindings.Get("pill", "1/berserk"); len(list) != 2 || list[0].ID != "30002" || list[1].ID != "2" {
		t.Errorf("new bindings %+v", list)
	}
	if list, _ := bindings.Get("asura", "b"); len(list) != 2 {
		t.Errorf("old bindings %+v", list)
	}
}

func TestBackfillSources(t *testing.T) {
	data := openData(t)
	library, watches := Library{DB: data}, Watches{DB: data}
	// adopted by a scan: no source
	for _, dir := range []string{"Berserk", "Vagabond", "Monster", "Pluto"} {
		if err := library.RecordDownload("/lib/p", dir, Download{Path: "/lib/p/" + dir + "/1.cbz", MangaTitle: dir, Chapter: 1, Format: "CBZ"}); err != nil {
			t.Fatal(err)
		}
	}
	berserk, _, _ := library.MangaByFolder("/lib/p", "Berserk")
	vagabond, _, _ := library.MangaByFolder("/lib/p", "Vagabond")
	monster, _, _ := library.MangaByFolder("/lib/p", "Monster")
	pluto, _, _ := library.MangaByFolder("/lib/p", "Pluto")
	// Berserk: a linked watch
	_, _ = watches.Add(Watch{Provider: "dex", MangaID: "b", Title: "Berserk", LibraryManga: berserk.ID})
	// Vagabond: downloads of two provider mangas, the latest wins
	for i, src := range []Source{{Provider: "old", MangaID: "v1"}, {Provider: "new", MangaID: "v2"}} {
		if _, err := data.Exec(`INSERT INTO downloads (path, chapter_id, provider, manga_id, manga_title, volume, chapter, chapter_title, chapter_url, format, pages, downloaded)
SELECT ?, c.id, ?, ?, 'Vagabond', 0, 1, '', '', 'PDF', 0, ? FROM library_chapters c WHERE c.manga = ?`, fmt.Sprintf("/lib/p/Vagabond/%d.pdf", i), src.Provider, src.MangaID, 100+i, vagabond.ID); err != nil {
			t.Fatal(err)
		}
	}
	// Monster: its watch's manga is another library manga's source already
	if err := library.RecordDownload("/lib/q", "Monster", Download{Path: "/lib/q/Monster/1.cbz", Provider: "dex", MangaID: "m", MangaTitle: "Monster", Chapter: 1, Format: "CBZ"}); err != nil {
		t.Fatal(err)
	}
	_, _ = watches.Add(Watch{Provider: "dex", MangaID: "m", Title: "Monster", LibraryManga: monster.ID})

	found, err := library.BackfillSources()
	if err != nil || found != (Backfill{FromWatches: 1, FromDownloads: 1}) {
		t.Fatalf("found %+v, %v", found, err)
	}
	if m, _, _ := library.MangaByID(berserk.ID); len(m.Sources) != 1 || m.Sources[0] != (Source{Provider: "dex", MangaID: "b", Title: "Berserk", Current: true}) {
		t.Errorf("berserk %+v", m.Sources)
	}
	if m, _, _ := library.MangaByID(vagabond.ID); len(m.Sources) != 1 || m.Sources[0].Provider != "new" || !m.Sources[0].Current {
		t.Errorf("vagabond %+v", m.Sources)
	}
	if m, _, _ := library.MangaByID(monster.ID); len(m.Sources) != 0 {
		t.Errorf("monster stole a source %+v", m.Sources)
	}
	if m, _, _ := library.MangaByID(pluto.ID); len(m.Sources) != 0 {
		t.Errorf("pluto %+v", m.Sources)
	}
	if found, _ := library.BackfillSources(); found != (Backfill{}) {
		t.Errorf("twice: %+v", found)
	}
	// a provider the manga has already isn't added again
	if ok, err := library.AddSource(berserk.ID, Source{Provider: "dex", MangaID: "b2"}); ok || err != nil {
		t.Errorf("second source of a provider: %t, %v", ok, err)
	}
	if ok, _ := library.AddSource(berserk.ID, Source{Provider: "pill", MangaID: "title:Berserk"}); ok {
		t.Error("added a placeholder id")
	}
}

func TestMergeKeepsCurrentSource(t *testing.T) {
	library := Library{DB: openData(t)}
	for i, dl := range []Download{
		{Path: "/lib/Berserk/1.cbz", Provider: "mango-weebcentral", MangaID: "w", MangaTitle: "Berserk", Chapter: 1, Format: "CBZ", Downloaded: time.Unix(100, 0)},
		// the merged-in manga downloaded later
		{Path: "/lib/Berserk 2/2.cbz", Provider: "mango-asurascans", MangaID: "a", MangaTitle: "Berserk", Chapter: 2, Format: "CBZ", Downloaded: time.Unix(200, 0)},
	} {
		if err := library.RecordDownload("/lib", filepath.Base(filepath.Dir(dl.Path)), dl); err != nil {
			t.Fatal(i, err)
		}
	}
	keep, _, _ := library.MangaByFolder("/lib", "Berserk")
	other, _, _ := library.MangaByFolder("/lib", "Berserk 2")
	if err := library.Merge(keep.ID, other.ID, []MergeMove{{From: "/lib/Berserk 2/2.cbz", To: "/lib/Berserk/2.cbz"}}, nil); err != nil {
		t.Fatal(err)
	}
	m, _, _ := library.MangaByID(keep.ID)
	if len(m.Sources) != 2 || m.Sources[0].Provider != "mango-weebcentral" || !m.Sources[0].Current || m.Sources[0].Moved.IsZero() {
		t.Errorf("sources %+v, want weebcentral still current", m.Sources)
	}
}

func TestSetLinks(t *testing.T) {
	data := openData(t)
	library, bindings := Library{DB: data}, Bindings{DB: data}
	al := func(raw string) metadata.ID {
		return metadata.ID{Raw: raw, Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}
	}
	info := libmangal.MangaDirInfo{Provider: "p", Manga: mangadata.MangaInfo{ID: "1", Title: "OPM"},
		MetadataIDs: []metadata.ID{al("1"), {Raw: "9", Source: metadata.IDSourceMyAnimeList, Code: metadata.IDCodeMyAnimeList}, al("2")}}
	if err := library.RememberMangaDir("/lib", "OPM", info); err != nil {
		t.Fatal(err)
	}
	if err := bindings.Set(Binding{Provider: "p", MangaID: "1", Source: metadata.IDCodeAnilist, ID: "1"}); err != nil {
		t.Fatal(err)
	}
	m, _, _ := library.MangaByFolder("/lib", "OPM")
	err := library.SetLinks(LinkChange{Manga: m.ID, Links: map[metadata.IDCode]string{
		metadata.IDCodeAnilist: "74347", metadata.IDCodeMyAnimeList: "", metadata.IDCodeKitsu: "24147",
	}, Bindings: [][2]string{{"p", "1"}}, From: "/lib/OPM", To: "/lib/One-Punch Man"})
	if err != nil {
		t.Fatal(err)
	}
	m, ok, _ := library.MangaByFolder("/lib", "One-Punch Man")
	if !ok {
		t.Fatal("folder not renamed")
	}
	want := []metadata.ID{al("74347"), {Raw: "24147", Source: metadata.IDSourceKitsu, Code: metadata.IDCodeKitsu}}
	if fmt.Sprint(m.MetadataIDs) != fmt.Sprint(want) {
		t.Errorf("ids %v, want %v", m.MetadataIDs, want)
	}
	got, _ := bindings.Get("p", "1")
	if len(got) != 2 || got[0].Source != metadata.IDCodeAnilist || got[0].ID != "74347" || got[1].ID != "24147" {
		t.Errorf("bindings %+v", got)
	}
	// the old id matches nothing any more
	old := libmangal.MangaDirInfo{Provider: "q", Manga: mangadata.MangaInfo{ID: "x"}, MetadataIDs: []metadata.ID{al("1")}}
	if _, found, _ := library.FindMangaDir("/lib", libmangal.ProviderInfo{ID: "q"}, old); found {
		t.Error("the old AniList id still finds the folder")
	}
}

func TestChapterNumbers(t *testing.T) {
	library := Library{DB: openData(t)}
	for _, dl := range []struct {
		dir string
		n   float32
	}{{"Berserk", 3}, {"Berserk", 1}, {"Berserk", 2.5}, {"Vagabond", 10}} {
		file := filepath.Join("/lib", dl.dir, fmt.Sprintf("%g.cbz", dl.n))
		if err := library.RecordDownload("/lib", dl.dir, Download{Path: file, Provider: "p", MangaID: dl.dir, MangaTitle: dl.dir, Chapter: dl.n, Format: "CBZ"}); err != nil {
			t.Fatal(err)
		}
	}
	// a chapter whose download is gone doesn't count
	if err := library.RemoveDownload(filepath.Join("/lib", "Vagabond", "10.cbz")); err != nil {
		t.Fatal(err)
	}
	numbers, err := library.ChapterNumbers()
	if err != nil {
		t.Fatal(err)
	}
	berserk, _, _ := library.MangaByFolder("/lib", "Berserk")
	if got := numbers[berserk.ID]; !slices.Equal(got, []float32{1, 2.5, 3}) || len(numbers) != 1 {
		t.Errorf("got %v", numbers)
	}
}

// FindMangaDir's own query finds what matching every manga of the parent finds.
func TestFindMangaDirLikeMangas(t *testing.T) {
	_, data := openBoth(t)
	library := Library{DB: data}
	anilist := func(raw string) []metadata.ID {
		return []metadata.ID{{Raw: raw, Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}}
	}
	for _, d := range []struct {
		dir  string
		info libmangal.MangaDirInfo
	}{
		{"Berserk", libmangal.MangaDirInfo{Provider: "mango-mangasee", Manga: mangadata.MangaInfo{ID: "berserk"}, MetadataIDs: anilist("1")}},
		{"Berserk", libmangal.MangaDirInfo{Provider: "q", Manga: mangadata.MangaInfo{ID: "b"}}},
		{"Scanned", libmangal.MangaDirInfo{MetadataIDs: anilist("2")}},
		{"Twin", libmangal.MangaDirInfo{Provider: "q", Manga: mangadata.MangaInfo{ID: "t"}, MetadataIDs: anilist("2")}},
		{"Elsewhere", libmangal.MangaDirInfo{Provider: "q", Manga: mangadata.MangaInfo{ID: "e"}}},
	} {
		if err := library.RememberMangaDir("/lib", d.dir, d.info); err != nil {
			t.Fatal(err)
		}
	}
	if err := library.RememberMangaDir("/other", "X", libmangal.MangaDirInfo{Provider: "q", Manga: mangadata.MangaInfo{ID: "x"}}); err != nil {
		t.Fatal(err)
	}
	mangas, err := library.mangas(`WHERE m.parent = ? AND m.dir != ''`, "/lib")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct {
		provider libmangal.ProviderInfo
		info     libmangal.MangaDirInfo
	}{
		{weebCentral, libmangal.MangaDirInfo{Provider: "mango-weebcentral", Manga: mangadata.MangaInfo{ID: "berserk"}}},
		{libmangal.ProviderInfo{ID: "q"}, libmangal.MangaDirInfo{Provider: "q", Manga: mangadata.MangaInfo{ID: "b"}}},
		{libmangal.ProviderInfo{ID: "r"}, libmangal.MangaDirInfo{Provider: "r", Manga: mangadata.MangaInfo{ID: "z"}, MetadataIDs: anilist("2")}},
		{libmangal.ProviderInfo{ID: "r"}, libmangal.MangaDirInfo{Provider: "r", Manga: mangadata.MangaInfo{ID: "z"}, MetadataIDs: anilist("1")}},
		{libmangal.ProviderInfo{ID: "q"}, libmangal.MangaDirInfo{Provider: "q", Manga: mangadata.MangaInfo{ID: "x"}}},
	} {
		best, want := libmangal.MangaDirNoMatch, ""
		for _, m := range mangas {
			haves := []libmangal.MangaDirInfo{{MetadataIDs: m.MetadataIDs}}
			for _, src := range m.Sources {
				haves = append(haves, libmangal.MangaDirInfo{Provider: src.Provider, Manga: mangadata.MangaInfo{ID: src.MangaID}, MetadataIDs: m.MetadataIDs})
			}
			for _, have := range haves {
				if match := libmangal.MatchMangaDir(q.provider, q.info, have); match > best {
					best, want = match, m.Dir
				}
			}
		}
		got, found, err := library.FindMangaDir("/lib", q.provider, q.info)
		if err != nil || got != want || found != (best != libmangal.MangaDirNoMatch) {
			t.Errorf("%+v: got %q %t %v, want %q", q.info, got, found, err, want)
		}
	}
}

// a fill keeps the folder's source current and doesn't link the other provider's manga
func TestRecordFill(t *testing.T) {
	library := Library{DB: openData(t)}
	home := Source{Provider: "mango-mangadex", MangaID: "md", Title: "Dark Gathering"}
	if err := library.RecordDownload("/lib", "Dark Gathering", Download{Path: "/lib/Dark Gathering/68.cbz", Provider: home.Provider, MangaID: home.MangaID, MangaTitle: home.Title, Chapter: 68, Format: "CBZ", Downloaded: time.Unix(100, 0)}); err != nil {
		t.Fatal(err)
	}
	fill := Download{Path: "/lib/Dark Gathering/1.cbz", Provider: "mango-mangapill", MangaID: "mp", MangaTitle: "Dark Gathering", Chapter: 1, ChapterURL: "https://mangapill.com/1", Format: "CBZ", Downloaded: time.Unix(200, 0),
		TitleProvider: home.Provider, TitleMangaID: home.MangaID, TitleChanged: time.Unix(200, 0)}
	if err := library.RecordFill("/lib", "Dark Gathering", home, fill); err != nil {
		t.Fatal(err)
	}
	m, _, _ := library.MangaByFolder("/lib", "Dark Gathering")
	if len(m.Sources) != 1 || m.Sources[0].Provider != home.Provider || !m.Sources[0].Current {
		t.Errorf("sources %+v, want mangadex only", m.Sources)
	}
	downloads, _ := library.Downloads(m.ID)
	if len(downloads) != 2 || downloads[0].Provider != "mango-mangapill" || downloads[0].MangaID != "mp" || downloads[0].ChapterURL != fill.ChapterURL ||
		downloads[0].TitleProvider != home.Provider || downloads[0].TitleMangaID != "md" || !downloads[0].TitleChanged.Equal(time.Unix(200, 0)) ||
		downloads[1].TitleProvider != "" || !downloads[1].TitleChanged.IsZero() {
		t.Errorf("downloads %+v", downloads)
	}
	// a retitle records where the title is from
	if err := library.Retitle(fill.Path, "/lib/Dark Gathering/1 Lie Low.cbz", "Lie Low", Source{Provider: "mango-comick", MangaID: "ck"}); err != nil {
		t.Fatal(err)
	}
	if downloads, _ := library.Downloads(m.ID); downloads[0].ChapterTitle != "Lie Low" || downloads[0].TitleProvider != "mango-comick" || downloads[0].TitleMangaID != "ck" || !downloads[0].TitleChanged.After(time.Unix(200, 0)) {
		t.Errorf("retitled %+v", downloads[0])
	}
	// a new folder gets home as its source
	if err := library.RecordFill("/lib", "New", Source{Provider: "mango-mangadex", MangaID: "new", Title: "New"}, Download{Path: "/lib/New/1.cbz", Provider: "mango-mangapill", MangaID: "mp2", Chapter: 1, Format: "CBZ"}); err != nil {
		t.Fatal(err)
	}
	if m, ok, _ := library.MangaBySource("mango-mangadex", "new"); !ok || m.Title != "New" || len(m.Sources) != 1 {
		t.Errorf("new folder %+v", m)
	}
}

// downloads from before migration 24 have no title source
func TestMigrateDownloadTitles(t *testing.T) {
	path := filepath.Join(t.TempDir(), FilenameData)
	old, err := Open(path, dataMigrations[:23])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`INSERT INTO library_mangas (id, title, parent, dir) VALUES (1, 'Berserk', '/lib', 'Berserk');
INSERT INTO library_chapters (id, manga, number) VALUES (1, 1, 1);
INSERT INTO downloads (path, chapter_id, provider, manga_id, manga_title, volume, chapter, chapter_title, chapter_url, format, pages, downloaded)
VALUES ('/lib/Berserk/1.cbz', 1, 'p', 'berserk', 'Berserk', 0, 1, 'One', '', 'CBZ', 0, 100)`); err != nil {
		t.Fatal(err)
	}
	old.Close()
	data, err := OpenData(path)
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	downloads, err := Library{DB: data}.Downloads(1)
	if err != nil || len(downloads) != 1 || downloads[0].ChapterTitle != "One" || downloads[0].TitleProvider != "" || !downloads[0].TitleChanged.IsZero() {
		t.Errorf("downloads %+v %v", downloads, err)
	}
}

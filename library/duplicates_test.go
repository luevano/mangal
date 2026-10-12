package library

import (
	"strings"
	"testing"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/storage"
)

func TestDuplicates(t *testing.T) {
	al := func(raw string) metadata.ID {
		return metadata.ID{Raw: raw, Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}
	}
	mal := func(raw string) metadata.ID {
		return metadata.ID{Raw: raw, Source: metadata.IDSourceMyAnimeList, Code: metadata.IDCodeMyAnimeList}
	}
	mangas := []storage.Manga{
		{ID: 1, Title: "OPM", Parent: "/dl/mango-mangadex", Dir: "OPM [alid-74347]", Chapters: 3, MetadataIDs: []metadata.ID{al("74347"), mal("44347")}},
		{ID: 2, Title: "One-Punch Man", Parent: "/dl/mango-mangadex", Dir: "One-Punch Man (2012) [alid-74347]", Chapters: 40,
			Sources: []storage.Source{{Provider: "mango-mangadex", MangaID: "d8a9", Current: true}}},
		{ID: 3, Title: "Berserk", Parent: "/dl/mango-mangadex", Dir: "Berserk", Chapters: 10, MetadataIDs: []metadata.ID{al("30002"), mal("2")}},
		{ID: 4, Title: "Berserk copy", Parent: "/dl/Weeb Central", Dir: "Berserk", Chapters: 12, MetadataIDs: []metadata.ID{mal("2")}},
		{ID: 5, Title: "Alone", Parent: "/dl", Dir: "Alone", MetadataIDs: []metadata.ID{al("1")}},
	}
	// one-punch man's AniList id is its source's binding, its MAL id the same as OPM's
	bindings := []storage.Binding{
		{Provider: "mango-mangadex", MangaID: "d8a9", Source: metadata.IDCodeAnilist, ID: "74347"},
		{Provider: "mango-mangadex", MangaID: "d8a9", Source: metadata.IDCodeMyAnimeList, ID: "44347"},
	}
	watched := func(m storage.Manga) bool { return m.ID == 3 }

	dups := Duplicates(mangas, bindings, watched)
	if len(dups) != 2 {
		t.Fatalf("got %d duplicates, want 2 (the AniList one and Berserk's MAL one): %+v", len(dups), dups)
	}
	if d := dups[0]; d.ID != metadata.IDCodeAnilist || d.Raw != "74347" || d.Keep().ID != 2 || d.Mangas[1].Manga.ID != 1 {
		t.Errorf("AniList: %+v, want 74347 keeping 2 (linked) over 1", d)
	}
	// watched wins over more chapters
	if d := dups[1]; d.ID != metadata.IDCodeMyAnimeList || d.Raw != "2" || d.Keep().ID != 3 || !d.Mangas[0].Watched {
		t.Errorf("MAL: %+v, want 2 keeping 3 (watched)", d)
	}
	ids := DuplicateIDs(dups)
	if len(ids) != 4 || ids[2] != 0 || ids[1] != 2 || ids[4] != 3 {
		t.Errorf("ids %v", ids)
	}

	got := strings.Join(DuplicateLines(dups, "/dl"), "\n")
	for _, want := range []string{
		"1 AniList id is in more than one folder (merge them: mangal library merge <keep> <other>):",
		"  AniList 74347:",
		"    keep 2  One-Punch Man  40 chapters  mango-mangadex  mango-mangadex/One-Punch Man (2012) [alid-74347]",
		"         1  OPM  3 chapters  no source  mango-mangadex/OPM [alid-74347]",
		"    mangal library merge 2 1",
		"1 MyAnimeList id is in more than one folder",
		"    keep 3  Berserk  10 chapters  no source, watched  mango-mangadex/Berserk",
		"    mangal library merge 3 4",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lines miss %q:\n%s", want, got)
		}
	}
	if Duplicates(mangas[2:3], nil, nil) != nil {
		t.Error("one manga has no duplicates")
	}
}

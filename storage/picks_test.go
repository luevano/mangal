package storage

import (
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
)

func TestLinkPicks(t *testing.T) {
	data := openData(t)
	library, bindings := Library{DB: data}, Bindings{DB: data}
	info := libmangal.MangaDirInfo{Provider: "mango-mangadex", Manga: mangadata.MangaInfo{ID: "md", Title: "OPM"}}
	if err := library.RememberMangaDir("/lib", "OPM", info); err != nil {
		t.Fatal(err)
	}
	m, _, _ := library.MangaByFolder("/lib", "OPM")
	pick := LinkPick{Manga: m.ID, Source: metadata.IDCodeKitsu, AniList: "3",
		Candidates: []LinkCandidate{{ID: "30", From: "mango-mangadex"}, {ID: "31", From: FromKitsu}}}
	if _, err := library.SetLinkPick(pick); err != nil {
		t.Fatal(err)
	}
	picks, err := library.LinkPicks()
	if err != nil || len(picks) != 1 || len(picks[0].Candidates) != 2 || picks[0].Candidates[1].FromName() != "Kitsu mapping" || picks[0].Found.IsZero() {
		t.Fatalf("picks %+v, %v", picks, err)
	}

	// dismissed: the same candidates found again stay dismissed, others ask again
	if ok, err := library.DismissLinkPick(m.ID, metadata.IDCodeKitsu); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if _, err := library.SetLinkPick(pick); err != nil {
		t.Fatal(err)
	}
	if picks, _ := library.LinkPicks(); len(picks) != 0 {
		t.Errorf("dismissed pick listed: %+v", picks)
	}
	if p, ok, _ := library.LinkPick(m.ID, metadata.IDCodeKitsu); !ok || !p.Dismissed {
		t.Errorf("pick %+v, %v: want dismissed", p, ok)
	}
	pick.Candidates[1].ID = "32"
	if _, err := library.SetLinkPick(pick); err != nil {
		t.Fatal(err)
	}
	if picks, _ := library.LinkPicks(); len(picks) != 1 {
		t.Errorf("new candidates not asked: %+v", picks)
	}

	// another AniList link clears it, a Kitsu link too
	if err := library.SetLinks(LinkChange{Manga: m.ID, Links: map[metadata.IDCode]string{metadata.IDCodeAnilist: "3"}}); err != nil {
		t.Fatal(err)
	}
	if picks, _ := library.LinkPicks(); len(picks) != 1 {
		t.Errorf("the same AniList id cleared the pick: %+v", picks)
	}
	if err := library.SetLinks(LinkChange{Manga: m.ID, Links: map[metadata.IDCode]string{metadata.IDCodeKitsu: "31"}}); err != nil {
		t.Fatal(err)
	}
	if picks, _ := library.LinkPicks(); len(picks) != 0 {
		t.Errorf("a Kitsu link kept the pick: %+v", picks)
	}
	if _, err := library.SetLinkPick(pick); err != nil {
		t.Fatal(err)
	}
	if err := library.SetLinks(LinkChange{Manga: m.ID, Links: map[metadata.IDCode]string{metadata.IDCodeAnilist: "4"}}); err != nil {
		t.Fatal(err)
	}
	if picks, _ := library.LinkPicks(); len(picks) != 0 {
		t.Errorf("another AniList link kept the pick: %+v", picks)
	}

	// binding its source to Kitsu clears it
	if _, err := library.SetLinkPick(pick); err != nil {
		t.Fatal(err)
	}
	if err := bindings.Set(Binding{Provider: "mango-mangadex", MangaID: "md", Source: metadata.IDCodeKitsu, ID: "30"}); err != nil {
		t.Fatal(err)
	}
	if picks, _ := library.LinkPicks(); len(picks) != 0 {
		t.Errorf("binding the source kept the pick: %+v", picks)
	}

	// gone with the manga
	if _, err := library.SetLinkPick(pick); err != nil {
		t.Fatal(err)
	}
	if err := library.RemoveManga(m.ID); err != nil {
		t.Fatal(err)
	}
	if picks, _ := library.LinkPicks(); len(picks) != 0 {
		t.Errorf("pick of a removed manga: %+v", picks)
	}
}

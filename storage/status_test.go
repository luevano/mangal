package storage

import (
	"testing"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
)

func TestStatuses(t *testing.T) {
	library := Library{DB: openData(t)}
	if err := library.RememberMangaDir("/lib", "DN", libmangal.MangaDirInfo{Provider: "p", Manga: mangadata.MangaInfo{ID: "1", Title: "DN"}}); err != nil {
		t.Fatal(err)
	}
	m, _, _ := library.MangaByFolder("/lib", "DN")
	if err := library.SetLinks(LinkChange{Manga: m.ID, Links: map[metadata.IDCode]string{metadata.IDCodeAnilist: "30021"}}); err != nil {
		t.Fatal(err)
	}
	if err := library.SetStatuses(map[int64]string{m.ID: string(metadata.StatusFinished)}, time.Unix(100, 0)); err != nil {
		t.Fatal(err)
	}
	if m, _, _ = library.MangaByID(m.ID); m.Status != "FINISHED" || m.StatusUpdated.Unix() != 100 {
		t.Errorf("status %q %v", m.Status, m.StatusUpdated)
	}
	// the same AniList id keeps it, another one forgets it
	_ = library.SetLinks(LinkChange{Manga: m.ID, Links: map[metadata.IDCode]string{metadata.IDCodeAnilist: "30021", metadata.IDCodeKitsu: "1"}})
	if m, _, _ = library.MangaByID(m.ID); m.Status != "FINISHED" {
		t.Errorf("status %q after the same link", m.Status)
	}
	_ = library.SetLinks(LinkChange{Manga: m.ID, Links: map[metadata.IDCode]string{metadata.IDCodeAnilist: "1"}})
	if m, _, _ = library.MangaByID(m.ID); m.Status != "" || !m.StatusUpdated.IsZero() {
		t.Errorf("status %q after another link", m.Status)
	}
}

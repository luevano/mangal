package storage

import (
	"testing"
	"time"

	"github.com/luevano/libmangal"
)

func TestDownloads(t *testing.T) {
	cache, data := openBoth(t)
	downloads := Downloads{DB: data}
	ch1 := Download{
		Path: "/lib/MangaSee/Berserk/[0001.0] Chapter 1.cbz", Provider: "mango-mangasee", MangaID: "berserk", MangaTitle: "Berserk",
		Volume: 1, Chapter: 1, ChapterTitle: "Chapter 1", ChapterURL: "https://x/1", Format: "CBZ", Pages: 20, Downloaded: time.Unix(100, 0),
	}
	ch2 := ch1
	ch2.Path, ch2.Chapter, ch2.ChapterTitle = "/lib/MangaSee/Berserk/[0002.0] Chapter 2.cbz", 2, "Chapter 2"
	other := Download{Path: "/lib/MangaSee/Vagabond/[0001.0] Chapter 1.cbz", Provider: "mango-mangasee", MangaID: "vagabond", MangaTitle: "Vagabond", Format: "CBZ"}
	for _, dl := range []Download{ch2, ch1, other} {
		if err := downloads.Record(dl); err != nil {
			t.Fatal(err)
		}
	}
	// skipped again (0 pages) keeps the page count
	again := ch1
	again.Pages = 0
	if err := downloads.Record(again); err != nil {
		t.Fatal(err)
	}

	list, err := downloads.List("mango-mangasee", "berserk")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Chapter != 1 || list[0].Pages != 20 || list[1].Chapter != 2 {
		t.Fatalf("got %+v", list)
	}
	if all, _ := downloads.List("", ""); len(all) != 3 {
		t.Errorf("%d downloads, want 3", len(all))
	}

	// library rename and migrate keep them in sync
	if err := (MangaDirs{DB: data}).Moved("/lib/MangaSee", "/lib/Weeb Central"); err != nil {
		t.Fatal(err)
	}
	weebCentral := libmangal.ProviderInfo{ID: "mango-weebcentral", Name: "Weeb Central", History: []libmangal.ProviderAlias{{ID: "mango-mangasee"}}}
	oldIDs, err := FindOldIDs(cache, data, []libmangal.ProviderLoader{loader{weebCentral}})
	if err != nil || len(oldIDs) != 1 || oldIDs[0].Downloads != 3 {
		t.Fatalf("old ids %+v, %v", oldIDs, err)
	}
	if err := MigrateOldIDs(cache, data, oldIDs); err != nil {
		t.Fatal(err)
	}
	list, _ = downloads.List("mango-weebcentral", "berserk")
	if len(list) != 2 || list[0].Path != "/lib/Weeb Central/Berserk/[0001.0] Chapter 1.cbz" {
		t.Errorf("after rename and migrate: %+v", list)
	}
}

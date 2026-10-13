package library

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/storage"
)

func statusMeta(status metadata.Status) metadata.Metadata {
	return &mangadata.Metadata{EnglishTitle: "x", PublicationStatus: status}
}

func TestStatusFinder(t *testing.T) {
	lib := newLibrary(t)
	for _, dir := range []string{"Death Note", "HxH", "Unlinked", "Old"} {
		if err := lib.RememberMangaDir("/lib", dir, libmangal.MangaDirInfo{Provider: "p", Manga: mangadata.MangaInfo{ID: dir, Title: dir}}); err != nil {
			t.Fatal(err)
		}
	}
	bindings := storage.Bindings(lib)
	_ = bindings.Set(storage.Binding{Provider: "p", MangaID: "Death Note", Source: "al", ID: "30021"})
	_ = bindings.Set(storage.Binding{Provider: "p", MangaID: "HxH", Source: "al", ID: "30026"})
	_ = bindings.Set(storage.Binding{Provider: "p", MangaID: "Old", Source: "al", ID: "1"})
	now := time.Unix(1_000_000, 0)
	old, _, _ := lib.MangaBySource("p", "Old")
	// looked up an hour ago: not asked again
	_ = lib.SetStatuses(map[int64]string{old.ID: string(metadata.StatusReleasing)}, now.Add(-time.Hour))

	var cachedAsk, fetchAsk [][]int
	finder := StatusFinder{
		Library: lib,
		Now:     func() time.Time { return now },
		Cached: func(ids []int) (map[int]metadata.Metadata, error) {
			cachedAsk = append(cachedAsk, slices.Clone(ids))
			return map[int]metadata.Metadata{30021: statusMeta(metadata.StatusFinished)}, nil
		},
		Fetch: func(_ context.Context, ids []int) (map[int]metadata.Metadata, error) {
			fetchAsk = append(fetchAsk, slices.Clone(ids))
			return map[int]metadata.Metadata{30026: statusMeta(metadata.StatusHiatus)}, nil
		},
	}
	all, _ := bindings.List("", "")
	mangas, _ := lib.Mangas()
	if err := finder.Update(context.Background(), mangas, all, false); err != nil {
		t.Fatal(err)
	}
	if len(fetchAsk) != 0 || len(cachedAsk) != 1 || len(cachedAsk[0]) != 2 {
		t.Errorf("cache asked %v, fetch %v", cachedAsk, fetchAsk)
	}
	status := func(title string) string {
		mangas, _ := lib.Mangas()
		for _, m := range mangas {
			if m.Title == title {
				return m.Status
			}
		}
		return "?"
	}
	if status("Death Note") != "FINISHED" || status("HxH") != "" || status("Old") != "RELEASING" {
		t.Errorf("after the cache: %q %q %q", status("Death Note"), status("HxH"), status("Old"))
	}
	mangas, _ = lib.Mangas()
	if err := finder.Update(context.Background(), mangas, all, true); err != nil {
		t.Fatal(err)
	}
	if len(fetchAsk) != 1 || !slices.Equal(fetchAsk[0], []int{30026}) {
		t.Errorf("fetched %v, want only the unknown one", fetchAsk)
	}
	if status("HxH") != "HIATUS" || StatusLabel(status("HxH")) != "hiatus" || !StatusDone(status("Death Note")) {
		t.Errorf("after the fetch: %q", status("HxH"))
	}
}

package client

import (
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
)

func TestRemoveMissingUndoable(t *testing.T) {
	lib, err := cache.Library()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Gone A", "Gone B"} {
		if err := lib.RememberMangaDir("/nowhere", name, libmangal.MangaDirInfo{Provider: "p", Manga: mangadata.MangaInfo{ID: name, Title: name}}); err != nil {
			t.Fatal(err)
		}
	}
	mangas, err := lib.Mangas()
	if err != nil {
		t.Fatal(err)
	}
	var missing []storage.Manga
	for _, m := range mangas {
		if m.Parent == "/nowhere" {
			missing = append(missing, m)
		}
	}
	if len(missing) != 2 {
		t.Fatalf("got %+v", mangas)
	}
	if _, err := RemoveMissing(lib, missing); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := lib.MangaByID(missing[0].ID); ok {
		t.Fatal("not removed")
	}
	undoer, err := LibraryUndoer()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := undoer.Plan(0)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Op.Kind != "remove" {
		t.Fatalf("last op %+v", plan.Op)
	}
	if _, _, err := Undo(undoer, plan, false); err != nil {
		t.Fatal(err)
	}
	for _, m := range missing {
		if _, ok, err := lib.MangaByID(m.ID); err != nil || !ok {
			t.Errorf("%d not back: %v", m.ID, err)
		}
	}
}

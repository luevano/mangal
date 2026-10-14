package storage

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
)

func TestHealthIgnores(t *testing.T) {
	data, err := OpenData(filepath.Join(t.TempDir(), FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	h := HealthIgnores{DB: data}
	if err := h.Dismiss(HealthIgnore{Check: "M3", Subject: "manga/5/al", Message: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := h.Dismiss(HealthIgnore{Check: "M3", Subject: "manga/5/al", Message: "y"}); err != nil {
		t.Fatal(err)
	}
	list, err := h.List()
	if err != nil || len(list) != 1 || list[0].Message != "y" || list[0].Dismissed.IsZero() {
		t.Fatalf("list %+v, %v", list, err)
	}
	if ok, err := h.Undismiss("M3", "manga/5/al"); !ok || err != nil {
		t.Errorf("undismiss: %v, %v", ok, err)
	}
	if ok, _ := h.Undismiss("M3", "manga/5/al"); ok {
		t.Error("undismissed twice")
	}
}

// TestRemovedMangaForgetsDismissals: a new manga reusing a removed one's id doesn't inherit
// its dismissed findings.
func TestRemovedMangaForgetsDismissals(t *testing.T) {
	data, err := OpenData(filepath.Join(t.TempDir(), FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	library := Library{DB: data}
	h := HealthIgnores{DB: data}
	add := func(title string) int64 {
		t.Helper()
		if err := library.RememberMangaDir("/lib", title, libmangal.MangaDirInfo{Manga: mangadata.MangaInfo{Title: title}}); err != nil {
			t.Fatal(err)
		}
		m, ok, err := library.MangaByFolder("/lib", title)
		if err != nil || !ok {
			t.Fatalf("%s: %v %v", title, ok, err)
		}
		return m.ID
	}
	id := add("Berserk")
	subject := fmt.Sprintf("manga/%d", id)
	for _, i := range []HealthIgnore{{Check: "M3", Subject: subject}, {Check: "M5", Subject: subject + "/al"}, {Check: "M3", Subject: fmt.Sprintf("manga/%d0", id)}} {
		if err := h.Dismiss(i); err != nil {
			t.Fatal(err)
		}
	}
	if err := library.RemoveManga(id); err != nil {
		t.Fatal(err)
	}
	list, err := h.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Subject != fmt.Sprintf("manga/%d0", id) {
		t.Errorf("left %+v, want only the other manga's", list)
	}
	if again := add("Vagabond"); again != id {
		t.Logf("id %d not reused (%d)", id, again)
	}
}

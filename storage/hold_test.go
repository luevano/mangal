package storage

import (
	"errors"
	"testing"
	"time"
)

func TestHold(t *testing.T) {
	data := openData(t)
	library, watches := Library{DB: data}, Watches{DB: data}
	for _, dir := range []string{"Berserk", "Berserk 2"} {
		if err := library.RecordDownload("/lib", dir, Download{Path: "/lib/" + dir + "/1.cbz", Provider: "p", MangaID: dir, MangaTitle: dir, Chapter: 1, Format: "CBZ"}); err != nil {
			t.Fatal(err)
		}
	}
	keep, _, _ := library.MangaByFolder("/lib", "Berserk")
	other, _, _ := library.MangaByFolder("/lib", "Berserk 2")

	if err := library.SetHold([]int64{keep.ID}, "paused"); err == nil {
		t.Error("set an unknown mark")
	}
	if err := library.SetHold([]int64{999}, HoldHiatus); !errors.Is(err, ErrMangaNotFound) {
		t.Errorf("set on a missing manga: %v", err)
	}
	if err := library.SetHold([]int64{other.ID}, HoldEnded); err != nil {
		t.Fatal(err)
	}
	if err := library.SetNewProvider(other.ID, "gone from p"); err != nil {
		t.Fatal(err)
	}
	if m, _, _ := library.MangaByID(other.ID); m.Hold != HoldEnded || m.NewProvider != "gone from p" {
		t.Errorf("manga %+v", m)
	}
	// the kept manga gets the marks it hasn't got
	if err := library.Merge(keep.ID, other.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	if m, _, _ := library.MangaByID(keep.ID); m.Hold != HoldEnded || m.NewProvider != "gone from p" {
		t.Errorf("merged %+v", m)
	}
	// a move to another provider clears the need of one
	if err := library.Move(MangaMove{Manga: keep.ID, Source: Source{Provider: "q", MangaID: "b", Title: "Berserk"}, From: "/lib/Berserk", To: "/lib/Berserk", At: time.Unix(1, 0)}); err != nil {
		t.Fatal(err)
	}
	if m, _, _ := library.MangaByID(keep.ID); m.Hold != HoldEnded || m.NewProvider != "" {
		t.Errorf("moved %+v", m)
	}

	w, err := watches.Add(Watch{Provider: "p", MangaID: "x", Title: "X", Hold: HoldHiatus, NeedsRelink: true, RelinkKind: RelinkProvider, RelinkReason: "has 60-63"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := watches.Get(w.ID)
	if got.Hold != HoldHiatus || got.RelinkKind != RelinkProvider || got.RelinkLabel() != "needs a new provider" {
		t.Errorf("added %+v", got)
	}
	// a check keeps the hold and the re-link kind, and writes the coverage
	got.Coverage, got.Hold = "has 1 of 20", ""
	if err := watches.SaveCheck(got); err != nil {
		t.Fatal(err)
	}
	if got, _ = watches.Get(w.ID); got.Hold != HoldHiatus || got.Coverage != "has 1 of 20" || got.RelinkKind != RelinkProvider {
		t.Errorf("after a check %+v", got)
	}
	if err := watches.Relink(w.ID, "q", "x", "X", 0, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if got, _ = watches.Get(w.ID); got.NeedsRelink || got.RelinkKind != "" || got.Coverage != "" || got.RelinkLabel() != "" {
		t.Errorf("re-linked %+v", got)
	}
	// the kind goes with the flag
	got.NeedsRelink, got.RelinkKind = false, RelinkProvider
	if err := watches.Save(got); err != nil {
		t.Fatal(err)
	}
	if got, _ = watches.Get(w.ID); got.RelinkKind != "" {
		t.Errorf("kind without the flag %+v", got)
	}
}

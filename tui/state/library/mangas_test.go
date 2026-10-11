package library

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
)

func press(k string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

func TestOnScanned(t *testing.T) {
	s := newMangasState(library.Files{Root: "/dl"})
	missing := []storage.Manga{{Title: "Berserk", Parent: "/old", Dir: "Berserk"}}

	if cmd := s.onScanned(scannedMsg{result: library.ScanResult{Missing: missing}, otherRoot: "/old"}); cmd != nil {
		t.Error("the other root shows a report, want the dialog only")
	}
	if !s.confirm.Active() || !strings.Contains(s.confirm.View(), "/old") {
		t.Fatal("no dialog for the other download path")
	}
	msg := s.confirm.Update(press("y"))()
	if got, ok := msg.(setRootMsg); !ok || got.dir != "/old" {
		t.Errorf("yes: got %#v, want setRootMsg{/old}", msg)
	}

	report := scanReport(library.ScanResult{Added: 2, Missing: missing, Unrecognized: []string{"/dl/x/extra.cbz"}})
	for _, want := range []string{"Added 2", "1 mangas weren't found", "Berserk  /old/Berserk", "1 files without a chapter number", "extra.cbz"} {
		if !strings.Contains(report, want) {
			t.Errorf("report misses %q:\n%s", want, report)
		}
	}
}

func TestDeleteKeepsTheMergeMarkUntilDone(t *testing.T) {
	s := newMangasState(library.Files{})
	items := []*mangaItem{{manga: storage.Manga{ID: 1, Title: "A"}}, {manga: storage.Manga{ID: 2, Title: "B"}}}
	s.onLoaded(mangasLoadedMsg{items: items})
	ctx := context.Background()

	s.Update(ctx, press("m"))
	if s.keep != 1 || !items[0].keep {
		t.Fatalf("keep = %d, want 1", s.keep)
	}
	s.Update(ctx, press("d"))
	if !s.confirm.Active() {
		t.Fatal("no delete dialog")
	}
	if s.keep != 1 {
		t.Error("the dialog cleared the merge mark")
	}
	s.Update(ctx, press("n"))
	if s.confirm.Active() || s.keep != 1 {
		t.Error("no: the mark should stay")
	}

	// deleted: the reload doesn't have it anymore
	s.onLoaded(mangasLoadedMsg{items: []*mangaItem{{manga: storage.Manga{ID: 2, Title: "B"}}}})
	if s.keep != 0 {
		t.Errorf("keep = %d after its manga left, want 0", s.keep)
	}
}

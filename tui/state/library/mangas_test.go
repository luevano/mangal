package library

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/tui/base"
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

func TestMangaWatches(t *testing.T) {
	ctx := context.Background()
	s := newMangaState(library.Files{Root: "/dl"}, storage.Manga{ID: 7, Title: "Berserk"})
	s.Resize(base.Size{Width: 100, Height: 30})
	if n, ok := s.Update(ctx, press("W"))().(base.NotificationMsg); !ok || n.Message != "Not watched" {
		t.Errorf("W not watched: %#v", n)
	}
	w := watch.Watch{Watch: storage.Watch{ID: 3, Title: "Berserk", Provider: "mango-mangapill", Enabled: true}, NextCheck: time.Now().Add(3*time.Hour + time.Minute)}
	paused := watch.Watch{Watch: storage.Watch{ID: 4, Title: "Berserk", Provider: "mango-mangadex"}, ProviderMissing: true}
	s.Update(ctx, mangaWatchesMsg{paused, w})
	view := s.View()
	for _, want := range []string{"watched on mango-mangapill: next check in 3h", "watched on mango-mangadex: paused, provider missing"} {
		if !strings.Contains(view, want) {
			t.Errorf("header misses %q:\n%s", want, view)
		}
	}
	if _, ok := s.Update(ctx, press("W"))().(base.State); !ok {
		t.Error("W on a watched manga isn't its watch")
	}
}

func TestFormatBindings(t *testing.T) {
	one := []storage.Binding{{Provider: "p", MangaID: "1", Source: "al", ID: "132029", Default: true}, {Provider: "p", MangaID: "1", Source: "mal", ID: "30230"}}
	if got := formatBindings(one); got != "al 132029 (default), mal 30230" {
		t.Errorf("one source: %q", got)
	}
	several := append(one, storage.Binding{Provider: "q", MangaID: "9", Source: "al", ID: "132029", Default: true}, storage.Binding{Source: "kt", ID: "38"})
	if got := formatBindings(several); got != "p: al 132029 (default), mal 30230; q: al 132029 (default); .libmangal.json: kt 38" {
		t.Errorf("several: %q", got)
	}
	if formatBindings(nil) != "" {
		t.Error("none")
	}
}

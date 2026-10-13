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
	"github.com/luevano/mangal/v5/tui/state/pick"
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
	if !s.Confirm.Active() || !strings.Contains(s.Confirm.View(), "/old") {
		t.Fatal("no dialog for the other download path")
	}
	msg := s.Confirm.Update(press("y"))()
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

	s.Update(ctx, press("J"))
	if s.keep != 1 || !items[0].keep {
		t.Fatalf("keep = %d, want 1", s.keep)
	}
	s.Update(ctx, press("d"))
	if !s.Confirm.Active() {
		t.Fatal("no delete dialog")
	}
	if s.keep != 1 {
		t.Error("the dialog cleared the merge mark")
	}
	s.Update(ctx, press("n"))
	if s.Confirm.Active() || s.keep != 1 {
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
	w := watch.Watch{Watch: storage.Watch{ID: 3, Title: "Berserk", Provider: "mango-mangapill", Enabled: true}, NextCheck: time.Now().Add(3*time.Hour + time.Minute), NextScheduled: time.Now().Add(3*time.Hour + time.Minute)}
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
	if got := formatBindings(several); got != "p: al 132029 (default), mal 30230; q: al 132029 (default); library: kt 38" {
		t.Errorf("several: %q", got)
	}
	if formatBindings(nil) != "" {
		t.Error("none")
	}
}

// TestKeys: no key means two things on a screen (its keys, the list's, the global ones).
func TestKeys(t *testing.T) {
	mangas := newMangasState(library.Files{})
	manga := newMangaState(library.Files{}, storage.Manga{ID: 7, Title: "Berserk"})
	trash := newTrashState(library.Files{})
	move := newMoveState(storage.Manga{ID: 7, Title: "Berserk"}, "mango-mangapill")
	links := newLinksState(storage.Manga{ID: 7, Title: "Berserk"})
	providers := pick.NewProviders("", nil)
	for name, keys := range map[string][]string{
		"move":      base.KeyCollisions(move.Keys(), move.List.KeyMap.Browsing()...),
		"providers": base.KeyCollisions(providers.Keys(), providers.List.KeyMap.Browsing()...),
		"mangas":    base.KeyCollisions(mangas.keyMap(), mangas.List.KeyMap.Browsing()...),
		"manga":     base.KeyCollisions(manga.keyMap(), manga.List.KeyMap.Browsing()...),
		"trash":     base.KeyCollisions(trash.keyMap(), trash.List.KeyMap.Browsing()...),
		"links":     base.KeyCollisions(links.keyMap(), links.List.KeyMap.Browsing()...),
	} {
		if len(keys) != 0 {
			t.Errorf("%s: %v", name, keys)
		}
	}
}

func TestSourcesShown(t *testing.T) {
	m := storage.Manga{ID: 2, Title: "Dungeon Odyssey", Parent: "/dl", Dir: "DO", Sources: []storage.Source{
		{Provider: "mango-weebcentral", MangaID: "01J", Moved: time.Date(2026, 10, 12, 4, 0, 0, 0, time.Local), Current: true},
		{Provider: "mango-asurascans", MangaID: "dungeon-odyssey"},
	}}
	if d := (&mangaItem{manga: m}).Description(); !strings.Contains(d, "mango-weebcentral (was mango-asurascans)") {
		t.Errorf("row %q", d)
	}
	h := newMangaState(library.Files{}, m).header()
	if !strings.Contains(h, "source: mango-weebcentral 01J · moved 2026-10-12\nprevious: mango-asurascans dungeon-odyssey") {
		t.Errorf("header %q", h)
	}
}

func TestDuplicatesFilter(t *testing.T) {
	ctx := context.Background()
	s := newMangasState(library.Files{})
	keep := storage.Manga{ID: 2, Title: "One-Punch Man", Dir: "OPM2", Chapters: 40}
	other := storage.Manga{ID: 1, Title: "OPM", Dir: "OPM1", Chapters: 3}
	dups := []library.Duplicate{{ID: "al", Raw: "74347", Mangas: []library.DuplicateManga{{Manga: keep}, {Manga: other}}}}
	items := []*mangaItem{
		{manga: storage.Manga{ID: 3, Title: "Berserk"}},
		{manga: other, duplicate: true, dupKeep: 2},
		{manga: keep, duplicate: true},
	}
	s.onLoaded(mangasLoadedMsg{items: items, dups: dups})
	if got := s.Subtitle(); !strings.Contains(got, "2 duplicates (D)") {
		t.Errorf("subtitle %q", got)
	}
	if !strings.Contains(items[1].Title(), "(duplicate)") {
		t.Errorf("title %q", items[1].Title())
	}
	s.Update(ctx, press("D"))
	shown := s.List.Items()
	if len(shown) != 2 || shown[0].(*mangaItem).manga.ID != 2 || shown[1].(*mangaItem).manga.ID != 1 {
		t.Fatalf("duplicates only: %v", shown)
	}
	// J on the other one merges it into its keep
	s.List.Select(1)
	s.Update(ctx, press("J"))
	if s.keep != 2 || !items[2].keep {
		t.Errorf("keep = %d, want the suggested 2", s.keep)
	}
	s.Update(ctx, press("D"))
	if len(s.List.Items()) != 3 {
		t.Errorf("all again: %d", len(s.List.Items()))
	}
}

func TestAskLinks(t *testing.T) {
	d := newDialog()
	d.Resize(base.Size{Width: 120, Height: 40})
	m := storage.Manga{ID: 4, Title: "Onepunch-Man", Parent: "/dl/mango-mangadex", Dir: "Onepunch-Man"}
	plan := library.LinkPlan{
		Info:    library.LinkInfo{Manga: m},
		Links:   library.Links{"al": "74347"},
		Changes: []library.LinkChange{{Code: "al", Old: []string{"21087"}, New: "74347"}},
		Rename:  &library.Rename{From: "/dl/mango-mangadex/Onepunch-Man", To: "/dl/mango-mangadex/One-Punch Man (2012) [alid-74347]"},
		Watches: []library.LinkWatch{{Watch: storage.Watch{ID: 4, Provider: "mango-weebcentral", MangaID: "w"}, Code: "al", Has: "21087", Want: "74347"}},
		Root:    "/dl",
	}
	if cmd := askLinks(d, "", linkPlannedMsg{plan: plan}); cmd != nil || !d.Active() {
		t.Fatal("no choice dialog")
	}
	view := d.View()
	for _, want := range []string{"AniList: 21087 -> 74347", "[alid-74347]", "watch 4", "link only", "rename the folder", "bind the watches", "all of it"} {
		if !strings.Contains(view, want) {
			t.Errorf("dialog misses %q:\n%s", want, view)
		}
	}
	// nothing to change nor imply
	plan.Changes, plan.Rename, plan.Watches = nil, nil, nil
	d = newDialog()
	if msg, ok := askLinks(d, "", linkPlannedMsg{plan: plan})().(base.NotificationMsg); !ok || d.Active() || !strings.Contains(msg.Message, "already") {
		t.Errorf("no change: %#v", msg)
	}
}

// TestDeleteMissing: d on a manga whose folder is gone forgets it, d (or y) confirms.
func TestDeleteMissing(t *testing.T) {
	s := newMangasState(library.Files{})
	s.onLoaded(mangasLoadedMsg{items: []*mangaItem{{manga: storage.Manga{ID: 1, Title: "A"}, missing: true}}})
	s.Update(context.Background(), press("d"))
	if !s.Confirm.Active() || !strings.Contains(s.Confirm.View(), "Its folder is gone") {
		t.Fatalf("d on a missing manga:\n%s", s.Confirm.View())
	}
}

// a merge asked for (health) while the library is in the history: that library does it
func TestRestoredWithMerge(t *testing.T) {
	s := newMangasState(library.Files{})
	// NewMerge's, without opening the library
	req := newMangasState(library.Files{})
	req.keep, req.duplicates, req.pendingMerge = 2, true, 1
	if req.Key() != s.Key() {
		t.Fatal("the merge's library isn't the library window")
	}
	s.Update(context.Background(), base.RestoredMsg{Request: req})
	if s.keep != 2 || !s.duplicates || s.pendingMerge != 1 {
		t.Errorf("keep %d, duplicates %v, pending %d", s.keep, s.duplicates, s.pendingMerge)
	}
}

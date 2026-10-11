package watches

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/tui/base"
)

type fakeManager struct {
	watches  []watch.Watch
	runs     []watch.CheckRun
	status   watch.Status
	checkErr error

	enabled map[int64]bool
	checked [][]int64
}

func (f *fakeManager) Add(ctx context.Context, opts watch.AddOptions) (watch.Watch, error) {
	return watch.Watch{}, nil
}
func (f *fakeManager) List(ctx context.Context) ([]watch.Watch, error) { return f.watches, nil }
func (f *fakeManager) Get(ctx context.Context, id int64) (watch.Watch, error) {
	for _, w := range f.watches {
		if w.ID == id {
			return w, nil
		}
	}
	return watch.Watch{}, watch.ErrNotFound
}
func (f *fakeManager) Remove(ctx context.Context, id int64) error { return nil }
func (f *fakeManager) SetEnabled(ctx context.Context, id int64, enabled bool) (watch.Watch, error) {
	if f.enabled == nil {
		f.enabled = map[int64]bool{}
	}
	f.enabled[id] = enabled
	return watch.Watch{}, nil
}
func (f *fakeManager) Update(ctx context.Context, id int64, opts watch.UpdateOptions) (watch.Watch, error) {
	return watch.Watch{}, nil
}
func (f *fakeManager) Check(ctx context.Context, ids []int64, opts watch.CheckOptions) (watch.Report, error) {
	f.checked = append(f.checked, ids)
	return watch.Report{CheckRun: storage.CheckRun{Watches: len(ids), NewChapters: 3}}, f.checkErr
}
func (f *fakeManager) Stale(ctx context.Context, since time.Duration) ([]watch.Watch, error) {
	return nil, nil
}
func (f *fakeManager) Runs(ctx context.Context, filter watch.RunFilter) ([]watch.CheckRun, error) {
	return f.runs, nil
}
func (f *fakeManager) RunDetails(ctx context.Context, id int64) (watch.CheckRun, error) {
	return watch.CheckRun{}, nil
}
func (f *fakeManager) Status(ctx context.Context) (watch.Status, error) { return f.status, nil }

var _ watch.Manager = (*fakeManager)(nil)

func press(k string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

var now = time.Date(2026, 10, 11, 12, 0, 0, 0, time.Local)

func ptr(n float32) *float32 { return &n }

func testManager() *fakeManager {
	return &fakeManager{
		watches: []watch.Watch{
			{Watch: storage.Watch{ID: 1, Title: "Berserk", Provider: "mango-mangapill", Enabled: true, LastChecked: now.Add(-2 * time.Hour)},
				LastDownloaded: ptr(380), NextCheck: now.Add(2 * time.Hour)},
			{Watch: storage.Watch{ID: 2, Title: "Dandadan", Provider: "mango-mangaplus", Enabled: false, Failures: 2, LastError: "boom"},
				NextCheck: now.Add(-time.Hour)},
			{Watch: storage.Watch{ID: 3, Title: "Frieren", Provider: "mango-mangaplus", Enabled: true},
				NextCheck: now.Add(-time.Minute)},
		},
		runs: []watch.CheckRun{
			{ID: 2, Kind: watch.KindManual, Started: now.Add(-2 * time.Hour), Results: []storage.WatchRun{
				{Watch: 1, Title: "Berserk", Status: storage.StatusNew, Started: now.Add(-2 * time.Hour), From: ptr(377),
					Chapters: []storage.RunChapter{{Number: 378, Title: "A"}, {Number: 379}, {Number: 380}}},
				{Watch: 3, Title: "Frieren", Status: storage.StatusSkipped},
			}},
			{ID: 1, Kind: watch.KindScheduled, Started: now.Add(-6 * time.Hour), Results: []storage.WatchRun{
				{Watch: 1, Title: "Berserk", Status: storage.StatusNone},
				{Watch: 3, Title: "Frieren", Status: storage.StatusError, Error: "timeout"},
			}},
		},
	}
}

// loaded is the watches screen with the fake's watches.
func loaded(t *testing.T, m *fakeManager) *listState {
	t.Helper()
	s := newListState(func(context.Context) (watch.Manager, error) { return m, nil })
	s.backoff = 2
	s.now = func() time.Time { return now }
	runs, _ := m.Runs(context.Background(), watch.RunFilter{})
	s.Update(context.Background(), loadedMsg{manager: m, status: m.status, watches: m.watches, last: lastResults(runs)})
	return s
}

func TestRows(t *testing.T) {
	s := loaded(t, testManager())
	items := s.List.Items()
	if len(items) != 3 {
		t.Fatalf("got %d rows, want 3", len(items))
	}
	want := []struct{ title, desc string }{
		{"Berserk", "mango-mangapill · last 380 · checked 2h ago · next in 2h · new 3"},
		{"Dandadan (paused)", "mango-mangaplus · last - · checked never · next paused · error · 2 failures"},
		// the skipped check doesn't count
		{"Frieren", "mango-mangaplus · last - · checked never · next due · error"},
	}
	for i, w := range want {
		it := items[i].(*item)
		if it.Title() != w.title || it.Description() != w.desc {
			t.Errorf("row %d:\n got %q / %q\nwant %q / %q", i, it.Title(), it.Description(), w.title, w.desc)
		}
	}
	if sub := s.Subtitle(); !strings.Contains(sub, "local") {
		t.Errorf("subtitle %q doesn't say local", sub)
	}
}

func TestBackingOff(t *testing.T) {
	w := watch.Watch{Watch: storage.Watch{Enabled: true, Failures: 3}, NextCheck: now.Add(5 * time.Hour)}
	if got := nextCheck(w, 2, now); got != "in 5h (backing off)" {
		t.Errorf("got %q", got)
	}
}

func TestPauseToggles(t *testing.T) {
	m := testManager()
	s := loaded(t, m)
	ctx := context.Background()

	for _, step := range []struct {
		index int
		want  bool
	}{{0, false}, {1, true}} {
		s.List.Select(step.index)
		cmd := s.Update(ctx, press("p"))
		if cmd == nil {
			t.Fatal("p: no cmd")
		}
		action, ok := cmd().(actionMsg)
		if !ok {
			t.Fatal("p: not an action")
		}
		if _, err := action.do(ctx); err != nil {
			t.Fatal(err)
		}
		id := m.watches[step.index].ID
		if got, ok := m.enabled[id]; !ok || got != step.want {
			t.Errorf("watch %d enabled = %v (set %v), want %v", id, got, ok, step.want)
		}
	}
}

func TestCheckBusy(t *testing.T) {
	m := testManager()
	m.status = watch.Status{Checking: true, Since: now.Add(-3 * time.Minute)}
	s := loaded(t, m)

	cmd := s.Update(context.Background(), press("c"))
	msg, ok := cmd().(base.NotificationMsg)
	if !ok || !strings.Contains(msg.Message, "running (since 3m ago)") {
		t.Errorf("c while checking: got %#v", cmd())
	}
	if len(m.checked) != 0 {
		t.Error("checked while busy")
	}
	if !strings.Contains(s.Subtitle(), "checking since 3m ago") {
		t.Errorf("subtitle %q", s.Subtitle())
	}

	// another process took the lock meanwhile
	m.checkErr = watch.ErrBusy
	got := checkCmd(context.Background(), m, []int64{1}, watch.CheckOptions{})()
	if n, ok := got.(base.NotificationMsg); !ok || !strings.Contains(n.Message, "running") {
		t.Errorf("busy check: got %#v", got)
	}
}

func TestCheckDue(t *testing.T) {
	m := testManager()
	s := loaded(t, m)
	cmd := s.Update(context.Background(), press("C"))
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("C: got %#v", cmd())
	}
	for _, c := range batch {
		if msg, ok := c().(base.NotificationWithDurationMsg); ok && !strings.Contains(msg.Message, "3 new chapters") {
			t.Errorf("summary %q", msg.Message)
		}
	}
	// the paused one is due but not checked
	if len(m.checked) != 1 || len(m.checked[0]) != 1 || m.checked[0][0] != 3 {
		t.Errorf("checked %v, want [[3]]", m.checked)
	}
	if !s.checking() {
		t.Error("a check just started doesn't count as running")
	}
}

func TestDetailsRender(t *testing.T) {
	m := testManager()
	s := newDetailsState(m, m.watches[0], 2)
	s.now = func() time.Time { return now }
	s.Resize(base.Size{Width: 120, Height: 30})
	s.Update(context.Background(), detailsMsg{w: m.watches[0], runs: m.runs})
	view := s.View()
	for _, want := range []string{"mango-mangapill", "last downloaded 380", "next check in 2h", "new 3 (378, 379, 380)", "from 377", "manual", "nothing"} {
		if !strings.Contains(view, want) {
			t.Errorf("details miss %q:\n%s", want, view)
		}
	}
	report := runReport(m.runs[0].Results[0])
	for _, want := range []string{"Status:   new 3", "From:     377", "378 - A"} {
		if !strings.Contains(report, want) {
			t.Errorf("report misses %q:\n%s", want, report)
		}
	}
}

func TestParse(t *testing.T) {
	if d, err := parseInterval("7d"); err != nil || d != 7*24*time.Hour {
		t.Errorf("7d: %v %v", d, err)
	}
	for _, bad := range []string{"30s", "x", "-1h"} {
		if _, err := parseInterval(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if f, err := parseFrom("All"); err != nil || f != nil {
		t.Errorf("all: %v %v", f, err)
	}
	if f, err := parseFrom("65.5"); err != nil || *f != 65.5 {
		t.Errorf("65.5: %v %v", f, err)
	}
	if _, err := parseAnilistID("abc"); err == nil {
		t.Error("abc accepted as an anilist id")
	}
	if got := defaultFrom(nil, ptr(70)); got != "70" {
		t.Errorf("default from the latest: %q", got)
	}
	if got := defaultFrom(ptr(60), ptr(70)); got != "60" {
		t.Errorf("default from the library: %q", got)
	}
	if got := defaultFrom(nil, nil); got != fromAll {
		t.Errorf("default without chapters: %q", got)
	}
}

func TestEditOptions(t *testing.T) {
	w := watch.Watch{Watch: storage.Watch{ID: 1, Title: "X", Gaps: true, AnilistID: 5}}
	s := newEditState(&fakeManager{}, w)
	if opts := s.options(); opts != (watch.UpdateOptions{}) {
		t.Errorf("unchanged: got %+v", opts)
	}
	s.interval.input.SetValue("12h")
	s.gaps.on = false
	s.anilist.input.SetValue("")
	opts := s.options()
	if opts.Interval == nil || *opts.Interval != 12*time.Hour || opts.Gaps == nil || *opts.Gaps || opts.AnilistID == nil || *opts.AnilistID != 0 {
		t.Errorf("changed: got %+v", opts)
	}
	s.interval.input.SetValue("1s")
	if s.form.valid() || s.interval.err == "" {
		t.Error("1s interval valid")
	}
	if !strings.Contains(s.View(), "at least 1m") {
		t.Errorf("no error shown:\n%s", s.View())
	}
}

func TestSet(t *testing.T) {
	set := NewSet([]watch.Watch{{Watch: storage.Watch{Provider: "p", MangaID: "1", LibraryManga: 7}}})
	if !set.Has("p", "1") || set.Has("p", "2") {
		t.Error("Has")
	}
	if !set.HasLibrary(storage.Manga{ID: 7}) || !set.HasLibrary(storage.Manga{ID: 8, Sources: []storage.Source{{Provider: "p", MangaID: "1"}}}) {
		t.Error("HasLibrary")
	}
	if set.HasLibrary(storage.Manga{ID: 9}) {
		t.Error("HasLibrary of another manga")
	}
}

func TestCheckError(t *testing.T) {
	m := &fakeManager{checkErr: errors.New("db gone")}
	if _, ok := checkCmd(context.Background(), m, nil, watch.CheckOptions{})().(error); !ok {
		t.Error("a failed check isn't an error msg")
	}
}

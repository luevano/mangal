package watches

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/luevano/mangal/v5/client/jobs"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/tui/base"
	tuijobs "github.com/luevano/mangal/v5/tui/jobs"
	"github.com/luevano/mangal/v5/tui/state/pick"
	"github.com/luevano/mangal/v5/tui/util"
)

type fakeManager struct {
	watches  []watch.Watch
	runs     []watch.CheckRun
	status   watch.Status
	checkErr error

	enabled  map[int64]bool
	mu       sync.Mutex
	checked  [][]int64
	relinked map[int64]watch.RelinkOptions
	// removedRuns are the watch runs RemoveRun was given
	removedRuns []int64
	// events is what Subscribe gives, nil for an error
	events chan watch.LiveEvent
}

func (f *fakeManager) Subscribe(ctx context.Context) (<-chan watch.LiveEvent, error) {
	if f.events == nil {
		return nil, errors.New("no events")
	}
	return f.events, nil
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
func (f *fakeManager) Relink(ctx context.Context, id int64, opts watch.RelinkOptions) (watch.Watch, error) {
	if f.relinked == nil {
		f.relinked = map[int64]watch.RelinkOptions{}
	}
	f.relinked[id] = opts
	return f.Get(ctx, id)
}
func (f *fakeManager) Check(ctx context.Context, ids []int64, opts watch.CheckOptions) (watch.Report, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checked = append(f.checked, ids)
	return watch.Report{CheckRun: storage.CheckRun{Watches: len(ids), NewChapters: 3}}, f.checkErr
}

// checks are the ids of each Check (they run in the background).
func (f *fakeManager) checks() [][]int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.checked)
}
func (f *fakeManager) Stale(ctx context.Context, since time.Duration) ([]watch.Watch, error) {
	return nil, nil
}
func (f *fakeManager) Runs(ctx context.Context, filter watch.RunFilter) ([]watch.CheckRun, error) {
	if filter.Provider == "" {
		return f.runs, nil
	}
	var runs []watch.CheckRun
	for _, run := range f.runs {
		var results []storage.WatchRun
		for _, r := range run.Results {
			if r.Provider == filter.Provider {
				results = append(results, r)
			}
		}
		if len(results) != 0 {
			run.Results = results
			runs = append(runs, run)
		}
	}
	return runs, nil
}
func (f *fakeManager) RunDetails(ctx context.Context, id int64) (watch.CheckRun, error) {
	return watch.CheckRun{}, nil
}
func (f *fakeManager) Status(ctx context.Context) (watch.Status, error) { return f.status, nil }
func (f *fakeManager) RemoveRun(ctx context.Context, id int64) (watch.RemovedRun, error) {
	f.removedRuns = append(f.removedRuns, id)
	return watch.RemovedRun{Run: storage.WatchRun{ID: id}, Snapshot: "/backups/x.sqlite"}, nil
}

var _ watch.Manager = (*fakeManager)(nil)

func press(k string) tea.KeyMsg {
	if r, ok := strings.CutPrefix(k, "alt+"); ok {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(r), Alt: true}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

var now = time.Date(2026, 10, 11, 12, 0, 0, 0, time.Local)

func ptr(n float32) *float32 { return &n }

func testManager() *fakeManager {
	return &fakeManager{
		watches: []watch.Watch{
			{Watch: storage.Watch{ID: 1, Title: "Berserk", Provider: "mango-mangapill", Enabled: true, LastChecked: now.Add(-2 * time.Hour)},
				LastDownloaded: ptr(380), NextCheck: now.Add(2 * time.Hour), NextScheduled: now.Add(2 * time.Hour)},
			{Watch: storage.Watch{ID: 2, Title: "Dandadan", Provider: "mango-mangaplus", Enabled: false, Failures: 2, LastError: "boom"},
				NextCheck: now.Add(-time.Hour), NextScheduled: now.Add(-time.Hour)},
			{Watch: storage.Watch{ID: 3, Title: "Frieren", Provider: "mango-mangaplus", Enabled: true},
				NextCheck: now.Add(-time.Minute), NextScheduled: now.Add(-time.Minute)},
		},
		runs: []watch.CheckRun{
			{ID: 2, Kind: watch.KindManual, Started: now.Add(-2 * time.Hour), Results: []storage.WatchRun{
				{Watch: 1, Title: "Berserk", Provider: "mango-mangapill", Status: storage.StatusNew, Started: now.Add(-2 * time.Hour), From: ptr(377),
					Chapters: []storage.RunChapter{{Number: 378, Title: "A"}, {Number: 379}, {Number: 380}}},
				{Watch: 3, Title: "Frieren", Provider: "mango-mangaplus", Status: storage.StatusSkipped},
			}},
			{ID: 1, Kind: watch.KindScheduled, Started: now.Add(-6 * time.Hour), Results: []storage.WatchRun{
				{Watch: 1, Title: "Berserk", Provider: "mango-mangapill", Status: storage.StatusNone},
				{Watch: 3, Title: "Frieren", Provider: "mango-mangaplus", Status: storage.StatusError, Error: "timeout"},
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
		{"Berserk", "mango-mangapill · last 380 · checked 2h ago · next in 2h (14:00) · new 3"},
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
	w := watch.Watch{Watch: storage.Watch{Enabled: true, Failures: 3}, NextCheck: now.Add(5 * time.Hour), NextScheduled: now.Add(5 * time.Hour)}
	if got := nextCheck(w, 2, now); got != "in 5h (17:00) (backing off)" {
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

// withJobs runs the checks' jobs in a manager of the test.
func withJobs(t *testing.T) *jobs.Manager {
	t.Helper()
	m := jobs.New(context.Background())
	tuijobs.SetManager(m)
	t.Cleanup(func() {
		m.Shutdown(time.Second)
		tuijobs.SetManager(nil)
	})
	return m
}

// jobsOver waits for every job and returns them.
func jobsOver(t *testing.T, m *jobs.Manager) []jobs.Job {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		changed := m.Changed()
		if len(m.Active()) == 0 {
			return m.Jobs()
		}
		select {
		case <-changed:
		case <-deadline:
			t.Fatalf("jobs still running: %v", m.Active())
		}
	}
}

// A check of a provider being checked is queued, not refused.
func TestCheckQueuesBusy(t *testing.T) {
	m := testManager()
	m.status = watch.Status{Checking: true, Since: now.Add(-3 * time.Minute),
		Providers: []watch.ProviderStatus{{Provider: "mango-mangapill", Since: now.Add(-3 * time.Minute)}}}
	jm := withJobs(t)
	s := loaded(t, m)
	ctx := context.Background()
	if !strings.Contains(s.Subtitle(), "1 provider checking") {
		t.Errorf("subtitle %q", s.Subtitle())
	}

	s.Update(ctx, press("u"))
	list := jobsOver(t, jm)
	if len(list) != 1 || list[0].Kind != jobs.KindCheck || !slices.Equal(list[0].Providers, []string{"mango-mangapill"}) || list[0].Origin != "watches" {
		t.Fatalf("jobs %+v", list)
	}
	if list[0].Status != jobs.Done || !strings.Contains(list[0].Message, "Checked 1 watch") {
		t.Errorf("job %s %q", list[0].Status, list[0].Message)
	}
	if len(m.checks()) != 1 {
		t.Errorf("checked %v", m.checks())
	}
}

// runBatch runs the cmds of a batch (not the ticks).
func runBatch(cmd tea.Cmd) []tea.Msg {
	var msgs []tea.Msg
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		return nil
	}
	for _, c := range batch {
		if c == nil {
			continue
		}
		done := make(chan tea.Msg, 1)
		go func() { done <- c() }()
		select {
		case msg := <-done:
			msgs = append(msgs, msg)
		case <-time.After(100 * time.Millisecond):
			// a tick
		}
	}
	return msgs
}

func TestCheckProvider(t *testing.T) {
	m := testManager()
	jm := withJobs(t)
	s := loaded(t, m)
	s.List.Select(2)
	base.Drain(s.Update(context.Background(), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c"), Alt: true}))
	list := jobsOver(t, jm)
	// the enabled watches of mangaplus: Frieren, not the paused Dandadan
	checked := m.checks()
	if len(checked) != 1 || !slices.Equal(checked[0], []int64{3}) || len(list) != 1 || !strings.Contains(list[0].Message, "Checked 1 watch") {
		t.Errorf("checked %v, jobs %+v", checked, list)
	}
}

func TestLiveMarks(t *testing.T) {
	m := testManager()
	m.events = make(chan watch.LiveEvent, 10)
	s := loaded(t, m)
	ctx := context.Background()
	f := &feed{ch: m.events}
	s.Update(ctx, feedMsg{feed: f})
	send := func(e watch.LiveEvent) {
		e.Time = now.Add(-time.Minute)
		s.Update(ctx, liveMsg{feed: f, e: e})
	}
	send(watch.LiveEvent{Run: 5, Kind: watch.LiveProviderStart, Provider: "mango-mangapill"})
	send(watch.LiveEvent{Run: 5, Kind: watch.LiveStart, Provider: "mango-mangapill", Watch: 1, Title: "Berserk"})
	n := float32(381)
	send(watch.LiveEvent{Run: 5, Kind: watch.LiveChapter, Provider: "mango-mangapill", Watch: 1, Chapter: &n, Message: "downloading chapter 381"})
	berserk := s.List.Items()[0].(*item)
	if !strings.Contains(berserk.Title(), "Berserk") || berserk.Title() == "Berserk" ||
		berserk.Description() != "mango-mangapill · last 380 · checking: downloading chapter 381 (1m)" {
		t.Errorf("checking row: %q / %q", berserk.Title(), berserk.Description())
	}
	if !strings.Contains(s.Subtitle(), "1 provider checking") || !s.spinning {
		t.Errorf("subtitle %q, spinning %v", s.Subtitle(), s.spinning)
	}
	// an event of an old subscription is dropped
	s.Update(ctx, liveMsg{feed: &feed{}, e: watch.LiveEvent{Kind: watch.LiveDone, Watch: 1}})
	if _, ok := berserk.checking(); !ok {
		t.Error("an old feed's event counted")
	}
	send(watch.LiveEvent{Run: 5, Kind: watch.LiveDone, Provider: "mango-mangapill", Watch: 1, Status: storage.StatusNew})
	send(watch.LiveEvent{Run: 5, Kind: watch.LiveProviderDone, Provider: "mango-mangapill"})
	if _, ok := berserk.checking(); ok || strings.Contains(s.Subtitle(), "checking") {
		t.Errorf("done: %q, %q", berserk.Title(), s.Subtitle())
	}

	// the status of a server's check, connected mid-run
	m.status = watch.Status{Providers: []watch.ProviderStatus{{Provider: "mango-mangaplus", Watch: 3, State: "found 12 chapters, nothing new"}}}
	s.Update(ctx, loadedMsg{manager: m, status: m.status, watches: m.watches})
	if d := s.List.Items()[2].(*item).Description(); !strings.Contains(d, "checking: found 12 chapters, nothing new") {
		t.Errorf("from the status: %q", d)
	}
}

func TestLiveLog(t *testing.T) {
	m := testManager()
	w := m.watches[0]
	s := newLiveState(m, &w)
	s.Resize(base.Size{Width: 80, Height: 3})
	ctx := context.Background()
	if !strings.Contains(s.View(), "isn't being checked") {
		t.Errorf("empty: %q", s.View())
	}
	f := &feed{ch: make(chan watch.LiveEvent)}
	s.Update(ctx, feedMsg{feed: f})
	for i, e := range []watch.LiveEvent{
		{Kind: watch.LiveStart, Watch: 1, Title: "Berserk", Message: "checking Berserk"},
		{Kind: watch.LiveStart, Watch: 3, Title: "Frieren", Message: "checking Frieren"},
		{Kind: watch.LiveFound, Watch: 1, Message: "found 381 chapters, 1 to download (381)"},
		{Kind: watch.LiveWait, Watch: 1, Message: "chapter 381 rate limited (retry #1), waiting 10s"},
		{Kind: watch.LiveChapterDone, Watch: 1, Message: "downloaded chapter 381"},
		{Kind: watch.LiveDone, Watch: 1, Message: "downloaded 1 (380 → 381)", Status: storage.StatusNew},
	} {
		e.Seq, e.Time = uint64(i+1), now
		s.Update(ctx, liveMsg{feed: f, e: e})
	}
	view := s.View()
	if strings.Contains(view, "Frieren") || !strings.Contains(view, "downloaded 1 (380 → 381)") || strings.Contains(view, "found 381") {
		t.Errorf("following, 3 lines:\n%s", view)
	}
	s.Update(ctx, press("k"))
	s.Update(ctx, press("k"))
	if view := s.View(); !strings.Contains(view, "found 381") || strings.Contains(view, "downloaded 1 (") {
		t.Errorf("scrolled up:\n%s", view)
	}
	s.Update(ctx, press("G"))
	if !strings.Contains(s.View(), "downloaded 1 (") {
		t.Error("G doesn't follow")
	}

	all := newLiveState(m, nil)
	all.Resize(base.Size{Width: 80, Height: 10})
	all.Update(ctx, feedMsg{feed: f})
	all.Update(ctx, liveMsg{feed: f, e: watch.LiveEvent{Kind: watch.LiveFound, Watch: 3, Title: "Frieren", Message: "found 12 chapters, nothing new", Time: now}})
	if !strings.Contains(all.View(), "Frieren: found 12 chapters") {
		t.Errorf("every check:\n%s", all.View())
	}
	// the page progress rewrites its line
	for _, msg := range []string{"chapter 13: 1/20 pages", "chapter 13: 2/20 pages"} {
		all.Update(ctx, liveMsg{feed: f, e: watch.LiveEvent{Kind: watch.LiveProgress, Watch: 3, Title: "Frieren", Message: msg, Time: now}})
	}
	all.Update(ctx, liveMsg{feed: f, e: watch.LiveEvent{Kind: watch.LiveChapterDone, Watch: 3, Title: "Frieren", Message: "downloaded chapter 13 (20 pages)", Time: now}})
	if view := all.View(); len(all.lines) != 3 || strings.Contains(view, "1/20") || !strings.Contains(view, "2/20 pages") || !strings.Contains(view, "(20 pages)") {
		t.Errorf("progress:\n%s", view)
	}
}

func TestCheckDue(t *testing.T) {
	m := testManager()
	jm := withJobs(t)
	s := loaded(t, m)
	s.Update(context.Background(), press("C"))
	list := jobsOver(t, jm)
	if len(list) != 1 || !strings.Contains(list[0].Message, "3 new chapters") {
		t.Errorf("jobs %+v", list)
	}
	// the paused one is due but not checked
	if checked := m.checks(); len(checked) != 1 || len(checked[0]) != 1 || checked[0][0] != 3 {
		t.Errorf("checked %v, want [[3]]", checked)
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
	for _, want := range []string{"mango-mangapill", "last downloaded 380", "next check in 2h", "metadata none", "new 3 (378, 379, 380)", "from 377", "manual", "nothing"} {
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

func TestBindings(t *testing.T) {
	w := watch.Watch{Bindings: []storage.Binding{{Source: "al", ID: "132029", Default: true}, {Source: "mal", ID: "30230"}}}
	if got := bindings(w); got != "al 132029 (default), mal 30230" {
		t.Errorf("got %q", got)
	}
}

func TestParse(t *testing.T) {
	for _, good := range []string{"", "0 */6 * * *", "@daily", "@every 12h"} {
		if err := validateSchedule(good); err != nil {
			t.Errorf("%q: %v", good, err)
		}
	}
	for _, bad := range []string{"12h", "* * *", "@every 30s", "61 * * * *"} {
		if err := validateSchedule(bad); err == nil {
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
	s.schedule.Input.SetValue(" 0 */12 * * * ")
	s.gaps.On = false
	s.anilist.Input.SetValue("")
	opts := s.options()
	if opts.Schedule == nil || *opts.Schedule != "0 */12 * * *" || opts.Gaps == nil || *opts.Gaps || opts.AnilistID == nil || *opts.AnilistID != 0 {
		t.Errorf("changed: got %+v", opts)
	}
	s.schedule.Input.SetValue("12h")
	if s.form.Valid() || s.schedule.Err == "" {
		t.Error("12h schedule valid")
	}
	if !strings.Contains(s.View(), "has 5 fields") {
		t.Errorf("no error shown:\n%s", s.View())
	}
}

func TestEditSkip(t *testing.T) {
	w := watch.Watch{Watch: storage.Watch{ID: 1, Title: "X", Skip: []float32{189.5}}}
	s := newEditState(&fakeManager{}, w)
	if s.skip.Value() != "189.5" || s.options().Skip != nil {
		t.Errorf("unchanged: %q, %+v", s.skip.Value(), s.options())
	}
	s.skip.Input.SetValue("190 189.5,190")
	if opts := s.options(); opts.Skip == nil || !slices.Equal(*opts.Skip, []float32{189.5, 190}) {
		t.Errorf("added: %+v", opts)
	}
	s.skip.Input.SetValue("")
	if opts := s.options(); opts.Skip == nil || len(*opts.Skip) != 0 {
		t.Errorf("cleared: %+v", opts)
	}
	s.skip.Input.SetValue("189.5, fake")
	if s.form.Valid() || s.skip.Err == "" {
		t.Error("fake accepted as a chapter number")
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
	jm := withJobs(t)
	m := &fakeManager{checkErr: errors.New("db gone")}
	queueCheck(m, []int64{1}, []string{"p"}, watch.CheckOptions{}, "1 watch", "watches")
	list := jobsOver(t, jm)
	if len(list) != 1 || list[0].Status != jobs.Failed || list[0].Err == nil || !strings.Contains(list[0].Err.Error(), "db gone") {
		t.Errorf("a failed check isn't a failed job: %+v", list)
	}
}

func TestSetFind(t *testing.T) {
	a := watch.Watch{Watch: storage.Watch{ID: 1, Provider: "p", MangaID: "1", LibraryManga: 7}}
	b := watch.Watch{Watch: storage.Watch{ID: 2, Provider: "q", MangaID: "9"}}
	set := NewSet([]watch.Watch{a, b})
	if w, ok := set.Find("q", "9"); !ok || w.ID != 2 {
		t.Errorf("Find: %+v %v", w, ok)
	}
	m := storage.Manga{ID: 7, Sources: []storage.Source{{Provider: "p", MangaID: "1"}, {Provider: "q", MangaID: "9"}}}
	if got := set.ForLibrary(m); len(got) != 2 || got[0].ID != 1 || got[1].ID != 2 {
		t.Errorf("ForLibrary: %+v", got)
	}
}

func TestJump(t *testing.T) {
	msg, ok := JumpCmd(nil, "w watches it")().(base.NotificationMsg)
	if !ok || msg.Message != "Not watched, w watches it" {
		t.Errorf("not watched: %#v", msg)
	}
	m := testManager()
	// the enabled one of the library manga's watches
	got := JumpCmd([]watch.Watch{m.watches[1], m.watches[2]}, "")()
	d, ok := got.(*detailsState)
	if !ok || d.w.ID != 3 || d.manager != nil {
		t.Fatalf("jump: got %#v", got)
	}
	// keys needing the manager wait for it
	if cmd := d.Update(context.Background(), press("e")); cmd != nil {
		t.Error("e before connecting")
	}
	d.Update(context.Background(), detailsMsg{manager: m, w: m.watches[2], runs: m.runs})
	if d.manager != m || len(d.List.Items()) != 2 {
		t.Errorf("loaded: manager %v, %d checks", d.manager, len(d.List.Items()))
	}
	if _, ok := d.Update(context.Background(), press("alt+h"))().(*historyState); !ok {
		t.Error("alt+h on the details isn't the history")
	}
}

func TestLibraryLink(t *testing.T) {
	found := storage.Manga{ID: 7, Title: "Berserk"}
	orig := findLibrary
	t.Cleanup(func() { findLibrary = orig })
	findLibrary = func(w watch.Watch) (storage.Manga, bool, error) {
		return found, w.LibraryManga == 7, nil
	}
	var opened storage.Manga
	screen := func(m storage.Manga) (base.State, error) {
		opened = m
		return newRunsState(nil), nil
	}

	w := watch.Watch{Watch: storage.Watch{Title: "Dandadan"}}
	msg, err := libraryScreen(screen, w)
	if n, ok := msg.(base.NotificationMsg); err != nil || !ok || !strings.Contains(n.Message, "Dandadan isn't in the library") {
		t.Errorf("missing: %#v, %v", msg, err)
	}
	w.LibraryManga = 7
	if msg, err := libraryScreen(screen, w); err != nil || msg == nil || opened.ID != 7 {
		t.Errorf("linked: %#v, %v, opened %+v", msg, err, opened)
	}

	origScreen := LibraryScreen
	LibraryScreen = screen
	t.Cleanup(func() { LibraryScreen = origScreen })
	if cmd := loaded(t, testManager()).Update(context.Background(), press("L")); cmd == nil {
		t.Error("L on a watch does nothing")
	}
}

func TestProviderHistory(t *testing.T) {
	m := testManager()
	ctx := context.Background()
	msg, err := loadHistory(ctx, m, "mango-mangaplus")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"mango-mangapill", "mango-mangaplus"}; !slices.Equal(msg.providers, want) {
		t.Errorf("providers %v, want %v", msg.providers, want)
	}
	var titles []string
	for _, i := range msg.items {
		titles = append(titles, i.(*runItem).Title())
	}
	// latest first, only the provider's
	want := []string{formatTime(time.Time{}) + " · Frieren · skipped", formatTime(time.Time{}) + " · Frieren · error"}
	if !slices.Equal(titles, want) {
		t.Errorf("history %q, want %q", titles, want)
	}

	// v on Dandadan: its provider's
	s := loaded(t, m)
	s.List.Select(1)
	h, ok := s.Update(ctx, press("alt+h"))().(*historyState)
	if !ok || h.provider != "mango-mangaplus" {
		t.Fatalf("alt+h: got %#v", h)
	}
	h.Update(ctx, msg)
	if len(h.List.Items()) != 2 || !strings.Contains(h.Subtitle(), "mango-mangaplus") {
		t.Errorf("history screen: %d items, subtitle %q", len(h.List.Items()), h.Subtitle())
	}
	// tab: every provider after the last one, then the first
	h.Update(ctx, tea.KeyMsg{Type: tea.KeyTab})
	if h.provider != "" {
		t.Errorf("tab: %q", h.provider)
	}
	h.Update(ctx, tea.KeyMsg{Type: tea.KeyTab})
	if h.provider != "mango-mangapill" {
		t.Errorf("tab again: %q", h.provider)
	}
	// a late result of the provider left behind is dropped
	h.Update(ctx, msg)
	if len(h.List.Items()) != 2 || !strings.Contains(h.Subtitle(), "mango-mangapill") {
		t.Errorf("subtitle %q", h.Subtitle())
	}
}

func TestProviderFilter(t *testing.T) {
	m := testManager()
	m.watches = append(m.watches, watch.Watch{Watch: storage.Watch{ID: 4, Title: "Akira", Provider: "mango-mangapill", Enabled: true}})
	s := loaded(t, m)
	ctx := context.Background()
	titles := func() []string {
		var titles []string
		for _, i := range s.List.Items() {
			titles = append(titles, i.(*item).w.Title)
		}
		return titles
	}
	s.Update(ctx, tea.KeyMsg{Type: tea.KeyTab})
	if got := titles(); !slices.Equal(got, []string{"Berserk", "Akira"}) || !strings.Contains(s.Subtitle(), "mango-mangapill") {
		t.Errorf("mangapill: %v, %q", got, s.Subtitle())
	}
	s.Update(ctx, tea.KeyMsg{Type: tea.KeyTab})
	if got := titles(); !slices.Equal(got, []string{"Dandadan", "Frieren"}) {
		t.Errorf("mangaplus: %v", got)
	}
	s.Update(ctx, tea.KeyMsg{Type: tea.KeyTab})
	s.Update(ctx, press("alt+o"))
	if got := titles(); !slices.Equal(got, []string{"Akira", "Berserk", "Dandadan", "Frieren"}) || !strings.Contains(s.Subtitle(), "by provider") {
		t.Errorf("by provider: %v, %q", got, s.Subtitle())
	}
}

type testManga struct{ info mangadata.MangaInfo }

func (m *testManga) String() string                     { return m.info.Title }
func (m *testManga) Info() mangadata.MangaInfo          { return m.info }
func (m *testManga) Metadata() metadata.Metadata        { return nil }
func (m *testManga) SetMetadata(meta metadata.Metadata) {}

func TestRelink(t *testing.T) {
	m := testManager()
	m.watches[1].MangaID, m.watches[1].NeedsRelink, m.watches[1].RelinkReason = "dandadan", true, "moved from mango-mangasee"
	m.watches[2].MangaID = "title:Frieren"
	m.watches = append(m.watches, watch.Watch{Watch: storage.Watch{ID: 4, Title: "Vagabond", Provider: "mango-gone", Enabled: true}, ProviderMissing: true})
	s := loaded(t, m)
	ctx := context.Background()

	if d := s.List.Items()[1].(*item); !strings.Contains(d.Title(), "needs re-link") || !strings.Contains(d.Description(), "needs re-link: moved from mango-mangasee") {
		t.Errorf("row %q / %q", d.Title(), d.Description())
	}
	if !strings.Contains(s.Subtitle(), "3 need a re-link") {
		t.Errorf("subtitle %q", s.Subtitle())
	}
	// only the ones to fix: flagged, placeholder id, provider missing
	s.Update(ctx, press("N"))
	if n := len(s.List.Items()); n != 3 || s.List.Items()[0].(*item).w.ID != 2 {
		t.Fatalf("filtered to %d", n)
	}
	cmd := s.Update(ctx, press("b"))
	if cmd == nil {
		t.Fatal("m: no cmd")
	}
	r, ok := cmd().(*relinkState)
	if !ok || r.Provider() != "mango-mangaplus" || r.Search.Query() != "Dandadan" {
		t.Fatalf("m: %#v", r)
	}
	s.Update(ctx, press("N"))
	if n := len(s.List.Items()); n != 4 {
		t.Errorf("unfiltered %d", n)
	}

	// the search on another provider, the current manga marked
	search := pick.SearchProvider
	t.Cleanup(func() { pick.SearchProvider = search })
	pick.SearchProvider = func(_ context.Context, provider, query string) ([]mangadata.Manga, string, error) {
		if old, ok := strings.CutPrefix(provider, "old-"); ok {
			provider = "mango-" + old
		}
		return []mangadata.Manga{
			&testManga{mangadata.MangaInfo{ID: "01J76", Title: query}},
			&testManga{mangadata.MangaInfo{ID: "dandadan", Title: query + " (old)"}},
		}, provider, nil
	}
	r.Update(ctx, pick.ProviderPickedMsg{Provider: "old-weebcentral"})
	res, err := r.Results(ctx, r.Provider(), "Dandadan")
	if err != nil || res.Provider != "mango-weebcentral" || len(res.Items) != 2 || res.Items[0].(*pick.MangaItem).Current {
		t.Fatalf("search %+v, %v", res, err)
	}
	// the search started by the pick
	res.Search = r.NewSearch()
	r.Update(ctx, res)
	if r.Provider() != "mango-weebcentral" || len(r.List.Items()) != 2 || !r.Assign.Enabled() {
		t.Errorf("results: %s, %d", r.Provider(), len(r.List.Items()))
	}
	if res, _ := r.Results(ctx, "mango-mangaplus", "Dandadan"); !res.Items[1].(*pick.MangaItem).Current {
		t.Error("the watch's manga isn't marked")
	}
	if cmd := r.Update(ctx, tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil {
		t.Error("enter: no cmd")
	}
	done, err := relink(ctx, m, 2, r.Provider(), r.List.Items()[0].(*pick.MangaItem).Manga)
	if err != nil || m.relinked[2] != (watch.RelinkOptions{Provider: "mango-weebcentral", MangaID: "01J76", Query: "Dandadan"}) || done.w.ID != 2 {
		t.Errorf("re-linked %+v, %v", m.relinked, err)
	}

	// the edit screen: enter on the manga opens the search, the re-link shows there
	e := newEditState(m, m.watches[1])
	if e.form.Focused() != e.schedule {
		t.Error("the edit screen doesn't start on the schedule")
	}
	e.Update(ctx, tea.KeyMsg{Type: tea.KeyShiftTab})
	if cmd := e.Update(ctx, tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil {
		t.Fatal("enter on the manga: no cmd")
	} else if _, ok := cmd().(*relinkState); !ok {
		t.Error("enter on the manga doesn't open the search")
	}
	moved := m.watches[1]
	moved.Provider, moved.MangaID, moved.NeedsRelink = "mango-weebcentral", "01J76", false
	e.Update(ctx, relinkedMsg{w: moved})
	if !strings.Contains(e.manga.Display, "mango-weebcentral · 01J76") || strings.Contains(e.manga.Display, "needs") {
		t.Errorf("edit after the re-link: %q", e.manga.Display)
	}
}

// TestKeys: no key means two things on a screen (its keys, the list's, the global ones).
func TestKeys(t *testing.T) {
	m := testManager()
	w := m.watches[0]
	l := loaded(t, m)
	d := newDetailsState(m, w, 2)
	h := newHistoryState(m, w.Provider)
	runs := newRunsState(m)
	run := newRunState(m, 1)
	r := newRelinkState(m, w)
	p := pick.NewProviders(w.Provider, nil)
	live := newLiveState(m, &w)
	add := newAddState(func(context.Context) (watch.Manager, error) { return m, nil }, nil, &testManga{mangadata.MangaInfo{ID: "1", Title: "Berserk"}}, nil)
	edit := newEditState(m, w)
	// enter changes the manga on its field, saves on the others
	enabled := func(km help.KeyMap) help.KeyMap {
		return util.KeyMap{Short: slices.DeleteFunc(util.Bindings(km), func(b key.Binding) bool { return !b.Enabled() })}
	}
	editSchedule := enabled(edit.KeyMap())
	edit.form.Focus(0)
	editManga := enabled(edit.KeyMap())
	for name, keys := range map[string][]string{
		"add":           base.KeyCollisions(add.KeyMap()),
		"edit schedule": base.KeyCollisions(editSchedule),
		"edit manga":    base.KeyCollisions(editManga),
		"list":          base.KeyCollisions(l.keyMap(), l.List.KeyMap.Browsing()...),
		"details":       base.KeyCollisions(d.keyMap(), d.List.KeyMap.Browsing()...),
		"history":       base.KeyCollisions(h.keyMap(), h.List.KeyMap.Browsing()...),
		"runs":          base.KeyCollisions(runs.keyMap(), runs.List.KeyMap.Browsing()...),
		"run":           base.KeyCollisions(run.keyMap(), run.List.KeyMap.Browsing()...),
		"relink":        base.KeyCollisions(r.Keys(), r.List.KeyMap.Browsing()...),
		"provider pick": base.KeyCollisions(p.Keys(), p.List.KeyMap.Browsing()...),
		"live":          base.ViewerKeyCollisions(live.KeyMap()),
	} {
		if len(keys) != 0 {
			t.Errorf("%s: %v", name, keys)
		}
	}
}

// TestStopWatching: d asks, d (or y) again removes the watch.
func TestStopWatching(t *testing.T) {
	s := loaded(t, testManager())
	ctx := context.Background()
	if cmd := s.Update(ctx, press("d")); cmd != nil || !s.Confirm.Active() {
		t.Fatal("d doesn't ask")
	}
	cmd := s.Update(ctx, press("d"))
	if cmd == nil {
		t.Fatal("d d: no cmd")
	}
	var action actionMsg
	for msg := cmd(); ; {
		if a, ok := msg.(actionMsg); ok {
			action = a
			break
		}
		batch, ok := msg.(tea.BatchMsg)
		if !ok || len(batch) == 0 {
			t.Fatalf("d d: %#v", msg)
		}
		msg = batch[len(batch)-1]()
	}
	if !strings.Contains(action.label, "Berserk") {
		t.Errorf("action %q", action.label)
	}
}

// TestLibraryIssue: the tag, its own color, the filter and the details.
func TestLibraryIssue(t *testing.T) {
	m := testManager()
	m.watches[2].LibraryIssue = "in the library under mango-mangasee (Sousou no Frieren): move it there (X in the library)"
	m.watches[2].LibraryElsewhere = true
	s := loaded(t, m)
	ctx := context.Background()
	if title := s.List.Items()[2].(*item).Title(); !strings.Contains(title, "(not in library)") {
		t.Errorf("title %q", title)
	}
	if title := s.List.Items()[1].(*item).Title(); !strings.Contains(title, "(paused)") || strings.Contains(title, "library") {
		t.Errorf("paused title %q", title)
	}
	colors := map[any]string{}
	for name, st := range map[string]lipgloss.Style{"re-link": relinkStyle, "library": libraryStyle, "paused": pausedStyle} {
		if other, ok := colors[st.GetForeground()]; ok {
			t.Errorf("%s and %s have the same color", name, other)
		}
		colors[st.GetForeground()] = name
	}
	if !strings.Contains(s.Subtitle(), "1 not in library (I)") {
		t.Errorf("subtitle %q", s.Subtitle())
	}
	s.Update(ctx, press("I"))
	if n := len(s.List.Items()); n != 1 || s.List.Items()[0].(*item).w.ID != 3 {
		t.Errorf("filtered to %d", n)
	}
	s.Update(ctx, press("I"))
	if n := len(s.List.Items()); n != 3 {
		t.Errorf("unfiltered %d", n)
	}
	if got := settings(m.watches[2], 3, now); !strings.Contains(got, "not in library: in the library under mango-mangasee (Sousou no Frieren): move it there (X in the library)") {
		t.Errorf("details %q", got)
	}
	if got := nextCheck(m.watches[2], 3, now); got != "not in library: move it" {
		t.Errorf("next check %q", got)
	}
	if got := mangaDisplay(m.watches[2]); !strings.Contains(got, "not in library: in the library under") {
		t.Errorf("edit %q", got)
	}
}

// with a filter on, the titles are plain (the list highlights matches by rune position)
func TestTitlePlainWhenFiltered(t *testing.T) {
	s := loaded(t, testManager())
	dandadan := s.List.Items()[1].(*item)
	s.List.SetFilterText("Dand")
	if title := dandadan.Title(); title != "Dandadan (paused)" {
		t.Errorf("filtered title %q", title)
	}
	_ = s.View()
}

// TestDeleteRun: d on a check asks, d (or y) again deletes it; the run screen goes back
// when the check run went with it.
func TestDeleteRun(t *testing.T) {
	m := testManager()
	ctx := context.Background()
	h := newHistoryState(m, "")
	msg, err := loadHistory(ctx, m, "")
	if err != nil {
		t.Fatal(err)
	}
	h.Update(ctx, msg)
	selected, ok := h.List.SelectedItem().(*runItem)
	if !ok {
		t.Fatal("no check selected")
	}
	if cmd := h.Update(ctx, press("d")); cmd != nil || !h.remove.confirm.Active() || h.Backable() || !h.Inputting() {
		t.Fatal("d doesn't ask")
	}
	if !strings.Contains(h.View(), "Delete check") {
		t.Errorf("no dialog:\n%s", h.View())
	}
	cmd := h.Update(ctx, press("d"))
	if cmd == nil {
		t.Fatal("d d: no cmd")
	}
	rm, ok := cmd().(removeRunMsg)
	if !ok || rm.r.ID != selected.r.ID || h.remove.confirm.Active() {
		t.Fatalf("d d: %#v", rm)
	}
	if cmd := h.Update(ctx, rm); cmd == nil {
		t.Error("no removal")
	}
	if cmd := h.Update(ctx, runRemovedMsg{notify: "deleted"}); cmd == nil {
		t.Error("no reload after the removal")
	}

	run := newRunState(m, 1)
	if cmd := run.afterRemove(ctx)(true); cmd == nil || cmd() != base.Back() {
		t.Error("the run screen doesn't go back once its run is gone")
	}
}

// a reload (the poll, back from another screen) keeps the filter and the watch selected
func TestReloadKeepsSelectionAndFilter(t *testing.T) {
	m := testManager()
	s := loaded(t, m)
	ctx := context.Background()
	s.List.Resize(base.Size{Width: 80, Height: 20})
	s.List.SetFilterText("mangaplus")
	s.List.SelectFunc(func(i _list.Item) bool { return i.(*item).w.ID == 3 })

	m.watches = append([]watch.Watch{{Watch: storage.Watch{ID: 4, Title: "Akira", Provider: "mango-mangaplus", Enabled: true}}}, m.watches...)
	s.Update(ctx, loadedMsg{manager: m, status: m.status, watches: m.watches})
	if s.List.FilterValue() != "mangaplus" || s.List.Unfiltered() {
		t.Fatalf("filter lost: %q", s.List.FilterValue())
	}
	if n := len(s.List.VisibleItems()); n != 3 {
		t.Errorf("%d shown, want the 3 of mangaplus", n)
	}
	if i, ok := s.List.SelectedItem().(*item); !ok || i.w.ID != 3 {
		t.Errorf("selected %v, want Frieren", s.List.SelectedItem())
	}
}

// the provider history asked for again (alt+h on another watch) shows that provider
func TestHistoryRequest(t *testing.T) {
	m := testManager()
	ctx := context.Background()
	h := newHistoryState(m, "mango-mangapill")
	cmd := h.Update(ctx, base.RestoredMsg{Request: newHistoryState(m, "mango-mangaplus")})
	if h.provider != "mango-mangaplus" || cmd == nil {
		t.Errorf("provider %q after the request", h.provider)
	}
	if h.Key() != newHistoryState(m, "x").Key() {
		t.Error("histories of two providers are two windows")
	}
}

// scrolled up to the oldest line kept while lines keep coming past the cap (MT-A1)
func TestLiveLogCapScrolled(t *testing.T) {
	m := testManager()
	s := newLiveState(m, nil)
	s.Resize(base.Size{Width: 80, Height: 10})
	ctx := context.Background()
	f := &feed{ch: make(chan watch.LiveEvent)}
	s.Update(ctx, feedMsg{feed: f})
	event := func(i int) {
		s.Update(ctx, liveMsg{feed: f, e: watch.LiveEvent{Kind: watch.LiveFound, Watch: 1, Message: fmt.Sprintf("line %d", i), Time: now}})
	}
	for i := range maxLiveLines {
		event(i)
	}
	s.Update(ctx, press("g"))
	s.Update(ctx, tea.KeyMsg{Type: tea.KeyPgUp})
	for range maxLiveLines {
		s.Update(ctx, tea.KeyMsg{Type: tea.KeyPgUp})
	}
	top := s.View()
	for i := range 50 {
		event(maxLiveLines + i)
		_ = s.View()
	}
	if s.offset > len(s.lines)-s.height() {
		t.Errorf("offset %d past the oldest line (%d lines)", s.offset, len(s.lines))
	}
	if got := s.View(); got == "" || got == top && !strings.Contains(got, "line 50") {
		t.Errorf("view at the top:\n%s", got)
	}
}

// the results of a search that a later one replaced are dropped: enter would re-link to
// a manga of the old search (MT-A7)
func TestRelinkDropsLateResults(t *testing.T) {
	m := testManager()
	ctx := context.Background()
	r := newRelinkState(m, m.watches[1])
	search := pick.SearchProvider
	t.Cleanup(func() { pick.SearchProvider = search })
	pick.SearchProvider = func(ctx context.Context, provider, query string) ([]mangadata.Manga, string, error) {
		return []mangadata.Manga{&testManga{mangadata.MangaInfo{ID: query, Title: query}}}, provider, nil
	}
	old, _ := r.Results(ctx, "mango-mangaplus", "old query")
	old.Search = r.NewSearch()
	latest, _ := r.Results(ctx, "mango-mangaplus", "new query")
	latest.Search = r.NewSearch()
	r.Update(ctx, latest)
	r.Update(ctx, old)
	if got := r.List.Items()[0].(*pick.MangaItem).Manga.Info().ID; got != "new query" {
		t.Errorf("shown %q, the late results replaced the latest", got)
	}
}

// removing a watch is undoable: the notification says the undo key
func TestRemoveSaysUndo(t *testing.T) {
	s := loaded(t, testManager())
	ctx := context.Background()
	s.List.Select(0)
	s.Update(ctx, press(base.KeyDelete.Help().Key))
	if !s.Confirm.Active() {
		t.Fatal("no confirmation")
	}
	action, ok := s.Update(ctx, press("y"))().(actionMsg)
	if !ok {
		t.Fatal("yes isn't the removal")
	}
	notify, err := action.do(ctx)
	if err != nil || !strings.Contains(notify, base.KeyUndo.Help().Key+" in the library undoes it") {
		t.Errorf("%q %v", notify, err)
	}
}

// back on the live log, the replay of the new subscription scrolls back to where it was
func TestLiveLogKeepsScroll(t *testing.T) {
	s := newLiveState(testManager(), nil)
	s.Resize(base.Size{Width: 80, Height: 5})
	ctx := context.Background()
	replay := func(f *feed, n int) {
		s.Update(ctx, feedMsg{feed: f})
		for i := range n {
			s.Update(ctx, liveMsg{feed: f, e: watch.LiveEvent{Kind: watch.LiveFound, Watch: 1, Message: fmt.Sprintf("line %d", i), Time: now}})
		}
	}
	replay(&feed{}, 20)
	for range 3 {
		s.Update(ctx, press("k"))
	}
	before := s.View()
	if !strings.HasPrefix(ansi.Strip(before), ansi.Strip(eventLine(watch.LiveEvent{Message: "line 12", Time: now}, true))) {
		t.Fatalf("scrolled up 3:\n%s", before)
	}

	s.Update(ctx, base.RestoredMsg{})
	replay(&feed{}, 22)
	if got := s.View(); got != before {
		t.Errorf("after the replay:\n%s\nwant\n%s", got, before)
	}
	// following, it follows again
	s.Update(ctx, press("G"))
	replay(&feed{}, 25)
	if !strings.Contains(s.View(), "line 24") {
		t.Errorf("following:\n%s", s.View())
	}
}

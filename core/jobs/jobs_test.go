package jobs

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/storage"
)

type chapter struct {
	mangadata.Chapter
	number float32
}

func (c chapter) Info() mangadata.ChapterInfo {
	return mangadata.ChapterInfo{Number: c.number, Title: fmt.Sprint("Chapter ", c.number), URL: fmt.Sprint("https://x/", c.number)}
}

func chapters(numbers ...float32) []mangadata.Chapter {
	var list []mangadata.Chapter
	for _, n := range numbers {
		list = append(list, chapter{number: n})
	}
	return list
}

// gated is a job that runs until its gate is closed, telling when it starts.
type gated struct {
	gate    chan struct{}
	started chan struct{}
	ctx     context.Context
}

func newGated() *gated { return &gated{gate: make(chan struct{}), started: make(chan struct{})} }

func (g *gated) spec(title string, providers ...string) Spec {
	return Spec{Kind: KindCheck, Title: title, Providers: providers, Run: func(ctx context.Context, p *Progress) error {
		g.ctx = ctx
		close(g.started)
		select {
		case <-g.gate:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
}

func newManager(t *testing.T) *Manager {
	t.Helper()
	m := New(context.Background())
	t.Cleanup(func() { m.Shutdown(time.Second) })
	return m
}

// waitFor waits until the job satisfies ok.
func waitFor(t *testing.T, m *Manager, id int64, what string, ok func(Job) bool) Job {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		changed := m.Changed()
		if j, found := m.Get(id); found && ok(j) {
			return j
		}
		select {
		case <-changed:
		case <-deadline:
			j, _ := m.Get(id)
			t.Fatalf("job %d (%s): not %s, status %s, message %q", id, j.Title, what, j.Status, j.Message)
		}
	}
}

func status(s Status) func(Job) bool { return func(j Job) bool { return j.Status == s } }

func started(t *testing.T, g *gated) {
	t.Helper()
	select {
	case <-g.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the job didn't start")
	}
}

func notStarted(t *testing.T, g *gated) {
	t.Helper()
	select {
	case <-g.started:
		t.Fatal("the job started")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestQueueOrder(t *testing.T) {
	m := newManager(t)
	a, b, c, other := newGated(), newGated(), newGated(), newGated()
	ja := m.Add(a.spec("A", "p"))
	jb := m.Add(b.spec("B", "p"))
	jc := m.Add(c.spec("C", "p"))
	m.Add(other.spec("other", "q"))

	started(t, a)
	// another provider doesn't wait
	started(t, other)
	notStarted(t, b)
	if jb.Status != Queued || jb.Ahead != 1 || jb.Behind != "A" {
		t.Errorf("B: %s, %d ahead, behind %q; want queued, 1, A", jb.Status, jb.Ahead, jb.Behind)
	}
	if jc.Ahead != 2 {
		t.Errorf("C: %d ahead, want 2", jc.Ahead)
	}
	if st, ok := m.MangaStatus("p", ""); ok {
		t.Errorf("a job without a manga marks one: %s", st)
	}

	close(a.gate)
	waitFor(t, m, ja.ID, "done", status(Done))
	started(t, b)
	notStarted(t, c)
	if j, _ := m.Get(jc.ID); j.Ahead != 1 || j.Behind != "B" {
		t.Errorf("C: %d ahead behind %q, want 1 behind B", j.Ahead, j.Behind)
	}
	close(b.gate)
	started(t, c)
	close(c.gate)
	waitFor(t, m, jc.ID, "done", status(Done))
}

// A check of two providers waits for both, the jobs after it wait behind it (FIFO), and
// the providers it's done with go on before it's over.
func TestQueueSeveralProviders(t *testing.T) {
	m := newManager(t)
	a, check, onQ, onP := newGated(), newGated(), newGated(), newGated()
	m.Add(a.spec("A", "p"))
	var release func(string)
	checkSpec := check.spec("check", "p", "q")
	run := checkSpec.Run
	checkSpec.Run = func(ctx context.Context, p *Progress) error {
		release = p.Release
		return run(ctx, p)
	}
	jcheck := m.Add(checkSpec)
	m.Add(onQ.spec("on q", "q"))
	m.Add(onP.spec("on p", "p"))
	started(t, a)
	// q is free, but the check is first in its queue
	notStarted(t, onQ)
	notStarted(t, check)

	close(a.gate)
	started(t, check)
	notStarted(t, onQ)
	release("q")
	started(t, onQ)
	notStarted(t, onP)
	close(check.gate)
	waitFor(t, m, jcheck.ID, "done", status(Done))
	started(t, onP)
	close(onQ.gate)
	close(onP.gate)
}

func TestCancel(t *testing.T) {
	m := newManager(t)
	a, b, c := newGated(), newGated(), newGated()
	ja := m.Add(a.spec("A", "p"))
	jb := m.Add(b.spec("B", "p"))
	jc := m.Add(c.spec("C", "p"))
	started(t, a)

	// a queued one leaves the queue
	if !m.Cancel(jb.ID) {
		t.Fatal("cancel of a queued job")
	}
	if j, _ := m.Get(jb.ID); j.Status != Cancelled {
		t.Errorf("B is %s", j.Status)
	}
	if j, _ := m.Get(jc.ID); j.Ahead != 1 {
		t.Errorf("C: %d ahead, want 1", j.Ahead)
	}
	// the running one stops, the next starts
	m.Cancel(ja.ID)
	waitFor(t, m, ja.ID, "cancelled", status(Cancelled))
	if a.ctx.Err() == nil {
		t.Error("A's ctx wasn't cancelled")
	}
	started(t, c)
	notStarted(t, b)
	if m.Cancel(ja.ID) {
		t.Error("cancel of a finished job")
	}
	close(c.gate)
}

func TestClearQueue(t *testing.T) {
	m := newManager(t)
	a, b, c, d, e := newGated(), newGated(), newGated(), newGated(), newGated()
	ja := m.Add(a.spec("A", "p"))
	jb := m.Add(b.spec("B", "p"))
	m.Add(c.spec("C", "p"))
	jd := m.Add(d.spec("D", "q"))
	je := m.Add(e.spec("E", "q"))
	started(t, a)
	started(t, d)

	if n := m.ClearQueue("p"); n != 2 {
		t.Errorf("cleared %d, want 2", n)
	}
	if j, _ := m.Get(jb.ID); j.Status != Cancelled {
		t.Errorf("B is %s", j.Status)
	}
	if j, _ := m.Get(ja.ID); j.Status != Running {
		t.Errorf("the running one is %s", j.Status)
	}
	if j, _ := m.Get(je.ID); j.Status != Queued {
		t.Errorf("q's queue: E is %s", j.Status)
	}
	if n := m.ClearQueue(""); n != 1 {
		t.Errorf("cleared %d of every queue, want 1", n)
	}
	if n := m.CancelAll(); n != 2 {
		t.Errorf("cancelled %d, want the 2 running", n)
	}
	waitFor(t, m, ja.ID, "cancelled", status(Cancelled))
	waitFor(t, m, jd.ID, "cancelled", status(Cancelled))
	if len(m.Active()) != 0 {
		t.Errorf("active after cancelling all: %v", m.Active())
	}
}

// downloadSpec is a download job of the chapters of manga "m" on p, download doing it.
func downloadSpec(title string, numbers []float32, download func(context.Context, []mangadata.Chapter, func(client.Event)) client.Results) Spec {
	return Spec{Kind: KindDownload, Title: title, Providers: []string{"p"}, MangaID: "m", Chapters: chapters(numbers...), Run: func(ctx context.Context, p *Progress) error {
		return RunDownload(ctx, p, download)
	}}
}

// fakeDownload downloads every chapter, failing the ones in fail with err.
func fakeDownload(fail map[float32]error) func(context.Context, []mangadata.Chapter, func(client.Event)) client.Results {
	return func(ctx context.Context, list []mangadata.Chapter, onEvent func(client.Event)) client.Results {
		results := client.NewResults(list)
		for i, r := range results {
			onEvent(client.Event{Kind: client.EventStart, Index: i, Result: r})
			if err := fail[r.Chapter.Info().Number]; err != nil {
				r.Err = err
			} else {
				r.Down = &metadata.DownloadedChapter{Directory: "/dl"}
			}
			onEvent(client.Event{Kind: client.EventDone, Index: i, Result: r})
		}
		return results
	}
}

func TestDownloadMarksAndRetry(t *testing.T) {
	m := newManager(t)
	gate := newGated()
	m.Add(gate.spec("first", "p"))
	started(t, gate)

	j := m.Add(downloadSpec("manga m", []float32{1, 2, 3}, fakeDownload(map[float32]error{2: errors.New("boom")})))
	if st, ok := m.ChapterStatus("p", "m", chapter{number: 2}); !ok || st != Queued {
		t.Errorf("chapter 2: %s %v, want queued", st, ok)
	}
	if _, ok := m.ChapterStatus("p", "m", chapter{number: 9}); ok {
		t.Error("a chapter not in the job is marked")
	}
	if st, ok := m.MangaStatus("p", "m"); !ok || st != Queued {
		t.Errorf("manga: %s %v, want queued", st, ok)
	}
	close(gate.gate)
	j = waitFor(t, m, j.ID, "failed", status(Failed))
	if !strings.Contains(j.Err.Error(), "1 chapter failed: boom") {
		t.Errorf("error %q", j.Err)
	}
	if j.Done != 3 || len(j.Results.Succeed()) != 2 || len(j.Results.Failed()) != 1 {
		t.Errorf("done %d, results %d ok %d failed", j.Done, len(j.Results.Succeed()), len(j.Results.Failed()))
	}
	if _, ok := m.MangaStatus("p", "m"); ok {
		t.Error("a finished download marks the manga")
	}

	// the retry downloads only the failed one
	var got []float32
	var mu sync.Mutex
	retried := false
	m.mu.Lock()
	e := m.findLocked(j.ID)
	e.run = func(ctx context.Context, p *Progress) error {
		return RunDownload(ctx, p, func(ctx context.Context, list []mangadata.Chapter, onEvent func(client.Event)) client.Results {
			mu.Lock()
			for _, c := range list {
				got = append(got, c.Info().Number)
			}
			retried = true
			mu.Unlock()
			return fakeDownload(nil)(ctx, list, onEvent)
		})
	}
	m.mu.Unlock()
	if _, err := m.Retry(j.ID); err != nil {
		t.Fatal(err)
	}
	j = waitFor(t, m, j.ID, "done", status(Done))
	mu.Lock()
	defer mu.Unlock()
	if !retried || !slices.Equal(got, []float32{2}) {
		t.Errorf("retried %v", got)
	}
	if len(j.Results.Succeed()) != 3 {
		t.Errorf("%d succeeded after the retry, want 3", len(j.Results.Succeed()))
	}
	if _, err := m.Retry(j.ID); !errors.Is(err, ErrNothingToRetry) {
		t.Errorf("retry without failures: %v", err)
	}
}

// A lock lost to another mangal fails the job with it, the next one goes on.
func TestLockLost(t *testing.T) {
	m := newManager(t)
	lost := fmt.Errorf("download stopped: %w", fmt.Errorf("lock provider:p: %w", storage.ErrLockLost))
	j := m.Add(downloadSpec("lost", []float32{1, 2}, func(ctx context.Context, list []mangadata.Chapter, onEvent func(client.Event)) client.Results {
		results := client.NewResults(list)
		onEvent(client.Event{Kind: client.EventStart, Index: 0, Result: results[0]})
		results[0].Err = lost
		onEvent(client.Event{Kind: client.EventDone, Index: 0, Result: results[0]})
		return results
	}))
	next := m.Add(downloadSpec("next", []float32{3}, fakeDownload(nil)))
	j = waitFor(t, m, j.ID, "failed", status(Failed))
	if !errors.Is(j.Err, storage.ErrLockLost) {
		t.Errorf("error %v, want the lost lock", j.Err)
	}
	if left := j.Results.ToDownload(); len(left) != 1 {
		t.Errorf("%d chapters left, want 1", len(left))
	}
	waitFor(t, m, next.ID, "done", status(Done))
}

// withLockStore makes the locks go to a temp data db, polling fast.
func withLockStore(t *testing.T) storage.Watches {
	t.Helper()
	db, err := storage.OpenData(filepath.Join(t.TempDir(), storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store := storage.Watches{DB: db}
	oldStore, oldPoll := lockStore, pollBusy
	lockStore = func() (storage.Watches, error) { return store, nil }
	pollBusy = 10 * time.Millisecond
	t.Cleanup(func() { lockStore, pollBusy = oldStore, oldPoll })
	return store
}

// A download whose provider another mangal has waits (client.Download's lock wait) and
// says who has it.
func TestDownloadWaitsForLock(t *testing.T) {
	store := withLockStore(t)
	lock, err := store.LockAs(client.ProviderLockName("p"), client.LockForWatch, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	m := newManager(t)
	j := m.Add(downloadSpec("waits", []float32{1}, func(ctx context.Context, list []mangadata.Chapter, onEvent func(client.Event)) client.Results {
		_, unlock, err := client.LockProvider(ctx, store, "p", client.LockForDownload, func(b client.ProviderBusy) {
			onEvent(client.Event{Kind: client.EventBusy, Busy: &b})
		})
		if err != nil {
			return client.NewResults(list)
		}
		defer unlock()
		return fakeDownload(nil)(ctx, list, onEvent)
	}))
	j = waitFor(t, m, j.ID, "waiting", status(Waiting))
	if want := "waiting for p (watch check, this mangal)"; j.Message != want {
		t.Errorf("message %q, want %q", j.Message, want)
	}
	if err := lock.Unlock(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, m, j.ID, "done", status(Done))
}

type fakeChecker struct {
	watch.Manager
	mu      sync.Mutex
	checked [][]int64
	errs    []error
}

func (f *fakeChecker) Check(ctx context.Context, ids []int64, opts watch.CheckOptions) (watch.Report, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checked = append(f.checked, ids)
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		return watch.Report{}, err
	}
	if opts.OnLive != nil {
		opts.OnLive(watch.LiveEvent{Kind: watch.LiveStart, Provider: "p", Message: "checking One Piece"})
		opts.OnLive(watch.LiveEvent{Kind: watch.LiveProviderDone, Provider: "p"})
	}
	return watch.Report{Watches: len(ids)}, nil
}

// A check waits while another mangal has its provider, and again when the check finds
// it busy after all.
func TestCheckWaitsForLock(t *testing.T) {
	store := withLockStore(t)
	lock, err := store.LockAs(client.ProviderLockName("p"), client.LockForDownload, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	m := newManager(t)
	checker := &fakeChecker{errs: []error{watch.ErrBusy}}
	j := m.Add(CheckSpec(checker, []int64{1, 2}, []string{"p"}, watch.CheckOptions{}, "2 watches", "watches", func(r watch.Report) string {
		return fmt.Sprintf("checked %d", r.Watches)
	}))
	j = waitFor(t, m, j.ID, "waiting", status(Waiting))
	if !strings.Contains(j.Message, "waiting for p (download") {
		t.Errorf("message %q", j.Message)
	}
	if err := lock.Unlock(); err != nil {
		t.Fatal(err)
	}
	j = waitFor(t, m, j.ID, "done", status(Done))
	if j.Message != "checked 2" {
		t.Errorf("message %q", j.Message)
	}
	checker.mu.Lock()
	defer checker.mu.Unlock()
	if len(checker.checked) != 2 {
		t.Errorf("checked %v, want twice (busy, then done)", checker.checked)
	}
}

func TestShutdown(t *testing.T) {
	m := New(context.Background())
	a, b := newGated(), newGated()
	m.Add(a.spec("A", "p"))
	jb := m.Add(b.spec("B", "p"))
	started(t, a)
	if !m.Shutdown(time.Second) {
		t.Fatal("the jobs didn't stop")
	}
	if a.ctx.Err() == nil {
		t.Error("the running job wasn't cancelled")
	}
	if j, _ := m.Get(jb.ID); j.Status != Cancelled {
		t.Errorf("the queued job is %s", j.Status)
	}
	if j := m.Add(newGated().spec("late", "q")); j.Status != Cancelled {
		t.Errorf("a job added after the shutdown is %s", j.Status)
	}
}

// TestOldProviderIDQueues: a job under a provider's old id queues behind the current id's.
func TestOldProviderIDQueues(t *testing.T) {
	old := resolveProvider
	t.Cleanup(func() { resolveProvider = old })
	resolveProvider = func(id string) string {
		if id == "mango-mangasee" {
			return "mango-weebcentral"
		}
		return id
	}
	m := newManager(t)
	first := newGated()
	a := m.Add(first.spec("current", "mango-weebcentral"))
	<-first.started
	second := newGated()
	b := m.Add(second.spec("old", "mango-mangasee"))
	if !slices.Equal(b.Providers, []string{"mango-weebcentral"}) {
		t.Errorf("providers %q, want the current id", b.Providers)
	}
	if j, _ := m.Get(b.ID); j.Status != Queued {
		t.Fatalf("old id's job %s, want queued behind the current id's", j.Status)
	}
	close(first.gate)
	waitFor(t, m, a.ID, "done", func(j Job) bool { return j.Status == Done })
	<-second.started
	close(second.gate)
	waitFor(t, m, b.ID, "done", func(j Job) bool { return j.Status == Done })
}

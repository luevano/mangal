package serve

import (
	"context"
	"io"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/storage"
)

type checkCall struct {
	ids  []int64
	kind string
	gaps bool
}

type fakeChecker struct {
	mu       sync.Mutex
	now      time.Time
	watches  []watch.Watch
	gapsDue  map[int64]bool
	busy     bool
	parallel int
	// block makes Check wait for it (or the ctx), started is told each check that starts
	block    chan struct{}
	started  chan struct{}
	calls    []checkCall
	canceled bool
}

func (f *fakeChecker) List(context.Context) ([]watch.Watch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.watches), nil
}

func (f *fakeChecker) GapsDue(w storage.Watch, _ time.Time) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gapsDue[w.ID]
}

func (f *fakeChecker) Parallel() int {
	if f.parallel == 0 {
		return 2
	}
	return f.parallel
}

func (f *fakeChecker) Check(ctx context.Context, ids []int64, opts watch.CheckOptions) (watch.Report, error) {
	f.mu.Lock()
	f.calls = append(f.calls, checkCall{ids, opts.Kind, opts.Gaps})
	id := int64(len(f.calls))
	busy, block, started := f.busy, f.block, f.started
	f.mu.Unlock()
	if busy {
		return watch.Report{}, watch.ErrBusy
	}
	opts.OnRun(storage.CheckRun{ID: id, Kind: opts.Kind})
	if started != nil {
		started <- struct{}{}
	}
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			f.mu.Lock()
			f.canceled = true
			f.mu.Unlock()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, w := range f.watches {
		if slices.Contains(ids, w.ID) {
			f.watches[i].NextCheck = f.now.Add(time.Hour)
			if opts.Gaps {
				delete(f.gapsDue, w.ID)
			}
		}
	}
	return watch.Report{CheckRun: storage.CheckRun{ID: id, Kind: opts.Kind}}, nil
}

func (f *fakeChecker) callList() []checkCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	calls := slices.Clone(f.calls)
	// providers run at once: in the order of their first watch
	slices.SortStableFunc(calls, func(a, b checkCall) int { return int(a.ids[0] - b.ids[0]) })
	return calls
}

func testWatch(id int64, next time.Time, enabled bool) watch.Watch {
	return providerWatch("p", id, next, enabled)
}

func providerWatch(provider string, id int64, next time.Time, enabled bool) watch.Watch {
	return watch.Watch{Watch: storage.Watch{ID: id, Title: "m", Provider: provider, Enabled: enabled}, NextCheck: next}
}

func newTestScheduler(f *fakeChecker) *Scheduler {
	s := newScheduler(func() checker { return f }, newLogger(io.Discard), &runs{})
	s.Now = func() time.Time {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.now
	}
	return s
}

// cycle runs a cycle and waits for the runs it started.
func cycle(s *Scheduler) time.Duration {
	sleep := s.cycle(context.Background())
	s.wg.Wait()
	return sleep
}

func TestSchedulerDue(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	f := &fakeChecker{now: now, watches: []watch.Watch{
		testWatch(1, now.Add(-time.Hour), true),
		testWatch(2, now, true),
		testWatch(3, now.Add(-time.Hour), false),
		testWatch(4, now.Add(30*time.Second), true),
		providerWatch("q", 5, now.Add(-time.Minute), true),
		providerWatch("r", 6, now.Add(-time.Minute), true),
	}}
	f.watches[5].ProviderMissing = true
	s := newTestScheduler(f)
	// a run per provider, the provider missing aside; p's watch 4 isn't counted while p runs
	if sleep := cycle(s); sleep != time.Minute {
		t.Errorf("sleep %s while p runs", sleep)
	}
	calls := f.callList()
	if len(calls) != 2 || !slices.Equal(calls[0].ids, []int64{1, 2}) || calls[0].kind != watch.KindScheduled || calls[0].gaps ||
		!slices.Equal(calls[1].ids, []int64{5}) {
		t.Fatalf("calls %+v", calls)
	}
	if r := s.Runs.running(); len(r) != 0 {
		t.Errorf("runs still marked running %v", r)
	}
	// p and q are free: until watch 4
	if sleep := cycle(s); sleep != 30*time.Second || len(f.callList()) != 2 {
		t.Errorf("sleep %s, calls %+v", sleep, f.callList())
	}

	// nothing due: no check, the longest sleep
	f.watches = f.watches[:2]
	if sleep := cycle(s); sleep != time.Minute || len(f.callList()) != 2 {
		t.Errorf("sleep %s, calls %+v", sleep, f.callList())
	}
}

// TestSchedulerParallel: providers run at once up to Parallel, never one twice.
func TestSchedulerParallel(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	f := &fakeChecker{now: now, parallel: 1, block: make(chan struct{}), started: make(chan struct{}, 10), watches: []watch.Watch{
		providerWatch("p", 1, now.Add(-time.Hour), true),
		providerWatch("q", 2, now.Add(-time.Hour), true),
	}}
	s := newTestScheduler(f)
	ctx := context.Background()
	s.cycle(ctx)
	<-f.started
	// p runs: q waits for the slot, p isn't started again
	if sleep := s.cycle(ctx); sleep != time.Minute {
		t.Errorf("sleep %s with every slot taken", sleep)
	}
	if calls := f.callList(); len(calls) != 1 || calls[0].ids[0] != 1 {
		t.Errorf("calls %+v", calls)
	}
	if r := s.Runs.running(); len(r) != 1 {
		t.Errorf("running %v", r)
	}
	f.mu.Lock()
	f.parallel = 2
	f.mu.Unlock()
	s.cycle(ctx)
	<-f.started
	if calls := f.callList(); len(calls) != 2 || calls[1].ids[0] != 2 {
		t.Errorf("q not started next to p: %+v", calls)
	}
	if r := s.Runs.running(); len(r) != 2 {
		t.Errorf("running %v", r)
	}
	close(f.block)
	s.wg.Wait()
	select {
	case <-s.wake:
	default:
		t.Error("a run that ended didn't wake the scheduler")
	}
}

func TestSchedulerBusy(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	f := &fakeChecker{now: now, busy: true, watches: []watch.Watch{testWatch(1, now.Add(-time.Hour), true)}}
	s := newTestScheduler(f)
	cycle(s)
	f.busy = false
	// tried again after MaxSleep
	if sleep := cycle(s); sleep != time.Minute || len(f.callList()) != 1 {
		t.Errorf("busy provider retried at once: %s %+v", sleep, f.callList())
	}
	f.now = f.now.Add(time.Minute)
	cycle(s)
	if calls := f.callList(); len(calls) != 2 || !slices.Equal(calls[1].ids, []int64{1}) {
		t.Errorf("not retried: %+v", calls)
	}
}

func TestSchedulerGaps(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	f := &fakeChecker{now: now, gapsDue: map[int64]bool{2: true, 1: true}, watches: []watch.Watch{
		testWatch(1, now.Add(-time.Minute), true),
		testWatch(2, now.Add(time.Hour), true),
		testWatch(3, now.Add(time.Hour), true),
	}}
	s := newTestScheduler(f)
	cycle(s)
	f.mu.Lock()
	calls := slices.Clone(f.calls)
	f.mu.Unlock()
	// 1 is due: its scheduled check fills its gaps (watch.Service does it), then p's gaps run
	want := []checkCall{{[]int64{1}, watch.KindScheduled, false}, {[]int64{2}, watch.KindGaps, true}}
	if len(calls) != 2 || !slices.Equal(calls[0].ids, want[0].ids) || calls[0].kind != want[0].kind ||
		!slices.Equal(calls[1].ids, want[1].ids) || calls[1].kind != want[1].kind || !calls[1].gaps {
		t.Errorf("calls %+v", calls)
	}
}

func TestSchedulerShutdown(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	f := &fakeChecker{now: now, block: make(chan struct{}), started: make(chan struct{}, 1),
		watches: []watch.Watch{testWatch(1, now.Add(-time.Hour), true)}}
	s := newTestScheduler(f)
	s.After = func(time.Duration) <-chan time.Time { return nil }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Run(ctx)
	}()
	<-f.started
	if r := s.Runs.running(); len(r) != 1 {
		t.Errorf("running %v", r)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run didn't stop")
	}
	if !f.canceled {
		t.Error("the check wasn't cancelled")
	}
}

func TestSchedulerWake(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	f := &fakeChecker{now: now}
	s := newTestScheduler(f)
	s.After = func(time.Duration) <-chan time.Time { return nil }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Run(ctx)
	}()
	f.mu.Lock()
	f.watches = []watch.Watch{testWatch(1, now, true)}
	f.mu.Unlock()
	s.Wake()
	deadline := time.Now().Add(5 * time.Second)
	for len(f.callList()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("not woken")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
}

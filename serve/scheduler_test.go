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
	mu      sync.Mutex
	now     time.Time
	watches []watch.Watch
	gapsDue map[int64]bool
	busy    bool
	// block makes Check wait for it (or the ctx)
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

func (f *fakeChecker) Check(ctx context.Context, ids []int64, opts watch.CheckOptions) (watch.Report, error) {
	f.mu.Lock()
	f.calls = append(f.calls, checkCall{ids, opts.Kind, opts.Gaps})
	busy, block, started := f.busy, f.block, f.started
	f.mu.Unlock()
	if busy {
		return watch.Report{}, watch.ErrBusy
	}
	opts.OnRun(storage.CheckRun{ID: int64(len(f.calls)), Kind: opts.Kind})
	if started != nil {
		close(started)
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
	return watch.Report{CheckRun: storage.CheckRun{ID: int64(len(f.calls)), Kind: opts.Kind}}, nil
}

func (f *fakeChecker) callList() []checkCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func testWatch(id int64, next time.Time, enabled bool) watch.Watch {
	return watch.Watch{Watch: storage.Watch{ID: id, Title: "m", Provider: "p", Enabled: enabled}, NextCheck: next}
}

func newTestScheduler(f *fakeChecker) *Scheduler {
	s := newScheduler(func() checker { return f }, newLogger(io.Discard), &runs{})
	s.Now = func() time.Time { return f.now }
	return s
}

func TestSchedulerDue(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	f := &fakeChecker{now: now, watches: []watch.Watch{
		testWatch(1, now.Add(-time.Hour), true),
		testWatch(2, now, true),
		testWatch(3, now.Add(-time.Hour), false),
		testWatch(4, now.Add(30*time.Second), true),
	}}
	s := newTestScheduler(f)
	sleep := s.cycle(context.Background())
	calls := f.callList()
	if len(calls) != 1 || !slices.Equal(calls[0].ids, []int64{1, 2}) || calls[0].kind != watch.KindScheduled || calls[0].gaps {
		t.Fatalf("calls %+v", calls)
	}
	if sleep != 30*time.Second {
		t.Errorf("sleep %s, want until watch 4", sleep)
	}
	if s.Runs.running() != 0 {
		t.Error("run still marked running")
	}

	// nothing due: no check, the longest sleep
	f.watches = f.watches[:2]
	if sleep := s.cycle(context.Background()); sleep != time.Minute || len(f.callList()) != 1 {
		t.Errorf("sleep %s, calls %+v", sleep, f.callList())
	}
}

func TestSchedulerBusy(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	f := &fakeChecker{now: now, busy: true, watches: []watch.Watch{testWatch(1, now.Add(-time.Hour), true)}}
	s := newTestScheduler(f)
	if sleep := s.cycle(context.Background()); sleep != time.Minute {
		t.Errorf("busy sleep %s", sleep)
	}
	f.busy = false
	s.cycle(context.Background())
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
	s.cycle(context.Background())
	calls := f.callList()
	// 1 is due: its scheduled check fills its gaps (watch.Service does it)
	want := []checkCall{{[]int64{1}, watch.KindScheduled, false}, {[]int64{2}, watch.KindGaps, true}}
	if len(calls) != 2 || !slices.Equal(calls[0].ids, want[0].ids) || calls[0].kind != want[0].kind ||
		!slices.Equal(calls[1].ids, want[1].ids) || calls[1].kind != want[1].kind || !calls[1].gaps {
		t.Errorf("calls %+v", calls)
	}
}

func TestSchedulerShutdown(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	f := &fakeChecker{now: now, block: make(chan struct{}), started: make(chan struct{}),
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
	if s.Runs.running() != 1 {
		t.Errorf("running %d", s.Runs.running())
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

package serve

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cron"
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

// NextCheckSince: a watch whose next check is before since waits a minute after since (its
// "next tick").
func (f *fakeChecker) NextCheckSince(w storage.Watch, since time.Time) time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, fw := range f.watches {
		if fw.ID == w.ID {
			if !since.IsZero() && fw.NextCheck.Before(since) {
				return since.Add(time.Minute)
			}
			return fw.NextCheck
		}
	}
	return time.Time{}
}

func (f *fakeChecker) NextGapCheckSince(w storage.Watch, _ time.Time) time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gapsDue[w.ID] {
		return f.now
	}
	return time.Time{}
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
	s.AuthChanged, s.AuthFingerprint = nil, nil
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
		watches: []watch.Watch{testWatch(1, now, true)}}
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

// tickChecker checks its watches, their next checks worked out by a watch.Service.
type tickChecker struct {
	fakeChecker
	svc    *watch.Service
	stored []storage.Watch
	// checks are "time ids kind" of each check
	checks []string
}

func (f *tickChecker) List(context.Context) ([]watch.Watch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var list []watch.Watch
	for _, w := range f.stored {
		list = append(list, watch.Watch{Watch: w, NextCheck: f.svc.NextCheck(w), NextGapCheck: f.svc.NextGapCheck(w)})
	}
	return list, nil
}

func (f *tickChecker) NextCheckSince(w storage.Watch, since time.Time) time.Time {
	return f.svc.NextCheckSince(w, since)
}

func (f *tickChecker) NextGapCheckSince(w storage.Watch, since time.Time) time.Time {
	return f.svc.NextGapCheckSince(w, since)
}

func (f *tickChecker) Check(ctx context.Context, ids []int64, opts watch.CheckOptions) (watch.Report, error) {
	opts.OnRun(storage.CheckRun{ID: 1, Kind: opts.Kind})
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks = append(f.checks, fmt.Sprintf("%s %v %s", f.now.Format(time.TimeOnly), ids, opts.Kind))
	for i, w := range f.stored {
		if slices.Contains(ids, w.ID) {
			// a scheduled check fills the gaps when they're due too (as watch.Service)
			if opts.Gaps || f.svc.GapsDue(w, f.now) {
				f.stored[i].LastGapCheck = f.now
			}
			f.stored[i].LastChecked = f.now
		}
	}
	return watch.Report{CheckRun: storage.CheckRun{ID: 1, Kind: opts.Kind}}, nil
}

// TestSchedulerTicks: checks run on the schedule's ticks, whenever the server started; a
// tick missed while it was down runs once at the start.
func TestSchedulerTicks(t *testing.T) {
	at := func(h, m, s int) time.Time { return time.Date(2026, 10, 11, h, m, s, 0, time.Local) }
	f := &tickChecker{
		svc: &watch.Service{Options: watch.Options{Schedule: cron.MustParse("*/2 * * * *"), GapSchedule: cron.MustParse("*/3 * * * *")}},
		stored: []storage.Watch{
			// last checked before the 10:00 tick, missed while down
			{ID: 1, Provider: "p", Enabled: true, LastChecked: at(9, 58, 30)},
			{ID: 2, Provider: "q", Enabled: true, LastChecked: at(10, 0, 40), Gaps: true, LastGapCheck: at(10, 0, 40)},
		},
	}
	f.now = at(10, 1, 10)
	s := newScheduler(func() checker { return f }, newLogger(io.Discard), &runs{})
	s.AuthChanged, s.AuthFingerprint = nil, nil
	s.Now = func() time.Time {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.now
	}
	// as Run does: started now, the 10:00 tick watch 1 missed while down isn't caught up
	s.started = f.now
	var sleeps []time.Duration
	for range 6 {
		sleep := cycle(s)
		sleeps = append(sleeps, sleep)
		f.mu.Lock()
		f.now = f.now.Add(sleep)
		f.mu.Unlock()
	}
	slices.Sort(f.checks)
	want := []string{
		"10:02:00 [1] scheduled", "10:02:00 [2] scheduled",
		"10:03:00 [2] gaps",
		"10:04:00 [1] scheduled", "10:04:00 [2] scheduled",
		"10:06:00 [1] scheduled", "10:06:00 [2] scheduled",
	}
	if !slices.Equal(f.checks, want) {
		t.Errorf("checks:\n%s\nwant:\n%s", strings.Join(f.checks, "\n"), strings.Join(want, "\n"))
	}
	if want := []time.Duration{50 * time.Second, time.Minute, time.Minute, time.Minute, time.Minute, time.Minute}; !slices.Equal(sleeps, want) {
		t.Errorf("sleeps %v, want %v", sleeps, want)
	}
}

func TestOptionsFrom(t *testing.T) {
	old := watch.Options{Schedule: cron.MustParse("@hourly")}
	opts := optionsFrom(old, map[string]any{
		config.Watch.Schedule.Key:    "*/2 * * * *",
		config.Watch.GapSchedule.Key: "0 3 * * 0",
		config.Watch.Jitter.Key:      "1m",
	})
	if opts.Schedule.String() != "*/2 * * * *" || opts.GapSchedule.String() != "0 3 * * 0" || opts.Jitter != time.Minute {
		t.Errorf("options %+v", opts)
	}
	opts = optionsFrom(old, map[string]any{config.Watch.Schedule.Key: "nope", config.Watch.GapSchedule.Key: ""})
	if opts.Schedule.String() != "@hourly" || !opts.GapSchedule.IsZero() {
		t.Errorf("invalid schedule: %+v", opts)
	}
}

// TestSchedulerNoCatchUp: starting doesn't run the watches whose ticks passed while mangal
// was down (nor new ones), they wait for their next tick.
func TestSchedulerNoCatchUp(t *testing.T) {
	at := func(h, m, s int) time.Time { return time.Date(2026, 10, 11, h, m, s, 0, time.Local) }
	f := &tickChecker{
		svc: &watch.Service{Options: watch.Options{Schedule: cron.MustParse("0 */4 * * *")}},
		stored: []storage.Watch{
			{ID: 1, Provider: "p", Enabled: true, LastChecked: at(4, 0, 30)}, // missed the 08:00 tick
			{ID: 2, Provider: "q", Enabled: true, Added: at(9, 0, 0)},        // never checked
		},
	}
	f.now = at(9, 30, 0)
	s := newScheduler(func() checker { return f }, newLogger(io.Discard), &runs{})
	s.AuthChanged, s.AuthFingerprint = nil, nil
	s.Now = func() time.Time {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.now
	}
	s.started = f.now
	cycle(s)
	if len(f.checks) != 0 {
		t.Fatalf("checked at start: %v", f.checks)
	}
	f.now = at(12, 0, 0)
	cycle(s)
	// the providers run in parallel, in any order
	slices.Sort(f.checks)
	if want := []string{"12:00:00 [1] scheduled", "12:00:00 [2] scheduled"}; !slices.Equal(f.checks, want) {
		t.Errorf("checks %v, want %v", f.checks, want)
	}
}

// TestSchedulerLibraryIssue: a watch with a library issue isn't checked, nor counted for
// the sleep.
func TestSchedulerLibraryIssue(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	f := &fakeChecker{now: now, watches: []watch.Watch{testWatch(1, now.Add(-time.Hour), true), testWatch(2, now.Add(10*time.Second), true)}}
	f.watches[0].LibraryIssue, f.watches[0].LibraryElsewhere = "in the library under q (m): move it there (M in the library)", true
	f.watches[1].LibraryIssue = "its downloads aren't in the library (library scan?)"
	s := newTestScheduler(f)
	if sleep := cycle(s); len(f.callList()) != 0 || sleep != time.Minute {
		t.Errorf("sleep %s, calls %+v", sleep, f.callList())
	}
}

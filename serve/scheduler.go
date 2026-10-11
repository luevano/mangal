package serve

import (
	"context"
	"errors"
	"time"

	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/storage"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// checker is what the scheduler needs of watch.Service.
type checker interface {
	List(ctx context.Context) ([]watch.Watch, error)
	GapsDue(w storage.Watch, now time.Time) bool
	Check(ctx context.Context, ids []int64, opts watch.CheckOptions) (watch.Report, error)
}

// Scheduler checks the watches when they're due: every cycle (at most MaxSleep apart, or
// when the earliest watch is due) the due ones are checked in a scheduled run, then the
// ones whose gap filling is due in a gaps run. A run that finds another check running
// (ErrBusy, e.g. `mangal watch check`) is tried again the next cycle.
type Scheduler struct {
	// Checker is the watch service to use for a cycle (its options may change between
	// cycles).
	Checker func() checker
	// Now is time.Now, After time.After (swapped in tests).
	Now   func() time.Time
	After func(time.Duration) <-chan time.Time
	// MaxSleep is the longest wait between cycles.
	MaxSleep time.Duration
	// Log is told what happens.
	Log logger
	// Runs tracks the running check.
	Runs *runs

	wake chan struct{}
}

// minSleep keeps a watch that stays due (e.g. its checks keep being skipped) from being
// checked in a loop.
const minSleep = 5 * time.Second

func newScheduler(c func() checker, log logger, r *runs) *Scheduler {
	return &Scheduler{
		Checker:  c,
		Now:      time.Now,
		After:    time.After,
		MaxSleep: time.Minute,
		Log:      log,
		Runs:     r,
		wake:     make(chan struct{}, 1),
	}
}

// Wake makes the scheduler look at the watches now (one was added or changed).
func (s *Scheduler) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Run schedules until ctx is cancelled, which also cancels the running check (it records
// what it got).
func (s *Scheduler) Run(ctx context.Context) {
	for {
		sleep := s.cycle(ctx)
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-s.After(sleep):
		case <-s.wake:
		}
	}
}

// cycle checks what's due and says how long to sleep.
func (s *Scheduler) cycle(ctx context.Context) time.Duration {
	c := s.Checker()
	now := s.Now()
	all, err := c.List(ctx)
	if err != nil {
		s.Log.Errorf("list the watches: %s", err)
		return s.MaxSleep
	}
	var due, gaps []int64
	for _, w := range all {
		switch {
		case !w.Enabled:
		case !w.NextCheck.After(now):
			due = append(due, w.ID)
		case c.GapsDue(w.Watch, now):
			gaps = append(gaps, w.ID)
		}
	}
	ok := true
	if len(due) > 0 {
		ok = s.check(ctx, c, due, watch.CheckOptions{Kind: watch.KindScheduled})
	}
	if len(gaps) > 0 && ctx.Err() == nil {
		ok = s.check(ctx, c, gaps, watch.CheckOptions{Kind: watch.KindGaps, Gaps: true}) && ok
	}
	sleep := s.MaxSleep
	if ctx.Err() != nil {
		return 0
	}
	if !ok {
		// busy or broken, the due watches stay due: next cycle
		return sleep
	}
	if len(due) > 0 || len(gaps) > 0 {
		if all, err = c.List(ctx); err != nil {
			return sleep
		}
	}
	now = s.Now()
	for _, w := range all {
		if w.Enabled {
			sleep = min(sleep, w.NextCheck.Sub(now))
		}
	}
	return max(sleep, minSleep)
}

// check runs one check, logging it, false when it couldn't run.
func (s *Scheduler) check(ctx context.Context, c checker, ids []int64, opts watch.CheckOptions) bool {
	opts.OnRun = func(run storage.CheckRun) {
		s.Runs.start(run.ID, nil)
		s.Log.Infof("%s check %d: %s", run.Kind, run.ID, stringutil.Quantify(len(ids), "watch", "watches"))
	}
	opts.OnEvent = logEvents(s.Log, false)
	report, err := c.Check(ctx, ids, opts)
	if report.ID != 0 {
		s.Runs.done(report.ID)
	}
	switch {
	case errors.Is(err, watch.ErrBusy):
		s.Log.Infof("%s check of %s postponed: %s", opts.Kind, stringutil.Quantify(len(ids), "watch", "watches"), err)
	case err != nil:
		s.Log.Errorf("%s check: %s", opts.Kind, err)
	default:
		logReport(s.Log, report)
		return true
	}
	return false
}

// logEvents logs each watch once checked and the rate limits waited out.
func logEvents(log logger, dryRun bool) func(watch.Event) {
	return func(e watch.Event) {
		switch e.Kind {
		case watch.EventChapter:
			if c := e.Chapter; c.Kind == client.EventWait {
				log.Infof("%s: chapter %s rate limited (retry #%d), retrying in %s", e.Watch.Title, c.Result.Chapter, c.Attempt, c.Wait)
			}
		case watch.EventDone:
			r := *e.Result
			line := watch.Describe(r, dryRun)
			if r.Error != "" {
				log.Warnf("%s", line)
			} else {
				log.Infof("%s", line)
			}
			for _, ch := range r.Chapters {
				if ch.Path != "" {
					log.Infof("  %s", ch.Path)
				}
			}
		}
	}
}

func logReport(log logger, report watch.Report) {
	took := report.Finished.Sub(report.Started).Round(time.Second)
	log.Infof("%s check %d done in %s: %s, %s, %s", report.Kind, report.ID, took,
		stringutil.Quantify(report.Watches, "watch", "watches"),
		stringutil.Quantify(report.NewChapters, "new chapter", "new chapters"),
		stringutil.Quantify(report.Errors, "error", "errors"))
}

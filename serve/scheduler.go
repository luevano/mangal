package serve

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/client/kitsu"
	"github.com/luevano/mangal/v5/client/myanimelist"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// checker is what the scheduler needs of watch.Service.
type checker interface {
	List(ctx context.Context) ([]watch.Watch, error)
	// NextCheckSince and NextGapCheckSince are when a watch (its gaps) is due for a
	// scheduler that started at since: ticks missed before aren't caught up.
	NextCheckSince(w storage.Watch, since time.Time) time.Time
	NextGapCheckSince(w storage.Watch, since time.Time) time.Time
	Check(ctx context.Context, ids []int64, opts watch.CheckOptions) (watch.Report, error)
	// Parallel is how many providers are checked at once (watch.parallel_providers).
	Parallel() int
}

// serviceChecker is the checker of a watch.Service.
type serviceChecker struct{ *watch.Service }

func (c serviceChecker) Parallel() int { return c.Options.ParallelProviders }

// Scheduler checks the watches when they're due, each provider on its own: every cycle
// (at most MaxSleep apart, at the next tick of a watch's schedule, or when a provider's
// run ends)
// each provider with due watches gets a scheduled run of them, then a gaps run of the
// ones whose gap filling is due, up to Parallel providers at once. A provider another
// check has (ErrBusy, e.g. `mangal watch check` or an API check) or whose run failed is
// tried again after MaxSleep.
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
	// Runs tracks the running checks.
	Runs *runs

	wake chan struct{}
	// started is when Run started: checks only start on the ticks after it
	started time.Time
	// auth is the logins/secrets fingerprint last seen (empty before the first cycle)
	auth string
	// AuthChanged runs before a provider's run: it reloads the provider when the saved
	// logins changed (swapped in tests)
	AuthChanged func(provider string) (bool, error)
	// AuthFingerprint is cache.AuthFingerprint (nil in tests: no reloads)
	AuthFingerprint func() (string, error)

	mu sync.Mutex
	// running are the providers being checked by the scheduler
	running map[string]bool
	// retry are the providers not to check again before then
	retry map[string]time.Time
	wg    sync.WaitGroup
}

// minSleep keeps a provider whose watches stay due (e.g. their checks keep being skipped)
// from being checked in a loop.
const minSleep = 5 * time.Second

func newScheduler(c func() checker, log logger, r *runs) *Scheduler {
	return &Scheduler{
		Checker:         c,
		Now:             time.Now,
		After:           time.After,
		MaxSleep:        time.Minute,
		Log:             log,
		Runs:            r,
		wake:            make(chan struct{}, 1),
		AuthChanged:     client.ReleaseIfAuthChanged,
		AuthFingerprint: cache.AuthFingerprint,
		running:         map[string]bool{},
		retry:           map[string]time.Time{},
	}
}

// Wake makes the scheduler look at the watches now (one was added or changed, a provider
// is free).
func (s *Scheduler) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Run schedules until ctx is cancelled, which also cancels the running checks (they
// record what they got); it returns once they're done.
func (s *Scheduler) Run(ctx context.Context) {
	defer s.wg.Wait()
	s.started = s.Now()
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

// work is what a provider's run checks.
type work struct {
	due, gaps []int64
}

// cycle starts the runs of the providers with something due and says how long to sleep.
func (s *Scheduler) cycle(ctx context.Context) time.Duration {
	c := s.Checker()
	now := s.Now()
	s.resetLogins()
	all, err := c.List(ctx)
	if err != nil {
		s.Log.Errorf("list the watches: %s", err)
		return s.MaxSleep
	}
	var providers []string
	todo := map[string]*work{}
	add := func(provider string) *work {
		if w, ok := todo[provider]; ok {
			return w
		}
		providers = append(providers, provider)
		todo[provider] = &work{}
		return todo[provider]
	}
	next := make(map[int64]time.Time, len(all))
	nextGaps := make(map[int64]time.Time, len(all))
	for _, w := range all {
		next[w.ID] = c.NextCheckSince(w.Watch, s.started)
		nextGaps[w.ID] = c.NextGapCheckSince(w.Watch, s.started)
		switch {
		case !w.Scheduled():
		case !next[w.ID].IsZero() && !next[w.ID].After(now):
			add(w.Provider).due = append(add(w.Provider).due, w.ID)
		case !nextGaps[w.ID].IsZero() && !nextGaps[w.ID].After(now):
			add(w.Provider).gaps = append(add(w.Provider).gaps, w.ID)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	parallel := max(1, c.Parallel())
	for _, p := range providers {
		if ctx.Err() != nil || len(s.running) >= parallel {
			break
		}
		if s.running[p] || now.Before(s.retry[p]) {
			continue
		}
		s.running[p] = true
		s.wg.Add(1)
		go s.runProvider(ctx, c, p, *todo[p])
	}
	if ctx.Err() != nil {
		return 0
	}

	// until the next tick of a watch (or its gaps) of a provider that can start; a running
	// provider, or one waiting for a free slot, wakes it when its run ends
	full := len(s.running) >= parallel
	sleep := s.MaxSleep
	for _, w := range all {
		if !w.Scheduled() || s.running[w.Provider] || (full && !next[w.ID].After(now)) {
			continue
		}
		next := next[w.ID]
		if g := nextGaps[w.ID]; !g.IsZero() && (next.IsZero() || g.Before(next)) {
			next = g
		}
		if next.IsZero() {
			continue
		}
		if r := s.retry[w.Provider]; next.Before(r) {
			next = r
		}
		sleep = min(sleep, next.Sub(now))
	}
	return max(sleep, minSleep)
}

// resetLogins makes the metadata providers log in again when the saved logins changed (a
// login from a TUI while serve runs).
func (s *Scheduler) resetLogins() {
	if s.AuthFingerprint == nil {
		return
	}
	fp, err := s.AuthFingerprint()
	if err != nil {
		return
	}
	if s.auth != "" && fp != s.auth {
		anilist.ResetLogin()
		kitsu.ResetLogin()
		myanimelist.ResetLogin()
		s.Log.Infof("logins or secrets changed")
	}
	s.auth = fp
}

// runProvider checks the provider's due watches, then the ones whose gaps are due.
func (s *Scheduler) runProvider(ctx context.Context, c checker, provider string, w work) {
	defer s.wg.Done()
	if s.AuthChanged != nil {
		if released, err := s.AuthChanged(provider); err != nil {
			s.Log.Errorf("%s: logins: %s", provider, err)
		} else if released {
			s.Log.Infof("logins or secrets changed, %s reloads with them", provider)
		}
	}
	ok := true
	if len(w.due) > 0 {
		ok = s.check(ctx, c, provider, w.due, watch.CheckOptions{Kind: watch.KindScheduled})
	}
	if len(w.gaps) > 0 && ctx.Err() == nil {
		ok = s.check(ctx, c, provider, w.gaps, watch.CheckOptions{Kind: watch.KindGaps, Gaps: true}) && ok
	}
	s.mu.Lock()
	delete(s.running, provider)
	wait := minSleep
	if !ok {
		// busy or broken: the due watches stay due
		wait = s.MaxSleep
	}
	s.retry[provider] = s.Now().Add(wait)
	s.mu.Unlock()
	s.Wake()
}

// check runs one check, logging it, false when it couldn't run.
func (s *Scheduler) check(ctx context.Context, c checker, provider string, ids []int64, opts watch.CheckOptions) bool {
	opts.OnRun = func(run storage.CheckRun) {
		s.Runs.start(run.ID, nil)
		s.Log.Infof("%s check %d of %s: %s", run.Kind, run.ID, provider, stringutil.Quantify(len(ids), "watch", "watches"))
	}
	opts.OnEvent = logEvents(s.Log, false)
	report, err := c.Check(ctx, ids, opts)
	if report.ID != 0 {
		s.Runs.done(report.ID)
	}
	switch {
	case errors.Is(err, watch.ErrBusy):
		s.Log.Infof("%s check of %s postponed: %s", opts.Kind, provider, err)
	case err != nil:
		s.Log.Errorf("%s check of %s: %s", opts.Kind, provider, err)
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
		case watch.EventBusy:
			log.Infof("%s: %s", e.Watch.Title, e.Busy.Message())
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

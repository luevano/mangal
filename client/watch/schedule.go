package watch

import (
	"context"
	"fmt"
	"hash/fnv"
	"slices"
	"time"

	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cron"
)

// maxBackoff caps how many ticks a failing watch skips (2^3 = it waits 8 ticks).
const maxBackoff = 3

// Schedule is the watch's schedule: its own, else its provider's (jobs.check.providers),
// else watch.schedule.
func (s *Service) Schedule(w storage.Watch) cron.Schedule {
	if w.Schedule != "" {
		sched, err := cron.Parse(w.Schedule)
		if err == nil {
			return sched
		}
		s.warn(fmt.Errorf("watch %d: %w, using watch.schedule", w.ID, err))
	}
	if sched, ok := s.Options.ProviderSchedules[w.Provider]; ok {
		return sched
	}
	if s.Options.Schedule.IsZero() {
		return DefaultSchedule
	}
	return s.Options.Schedule
}

// NextCheck is when the watch is due: the first tick of its schedule (watch.schedule if it
// has none) after the last check, or after it was resumed or re-linked when that's later
// (after it was added if neither happened), plus the jitter
// of its provider for that tick (watch.jitter). After watch.backoff failures in a row it
// skips ticks: it waits 2, 4, then 8 ticks.
func (s *Service) NextCheck(w storage.Watch) time.Time {
	since := w.LastChecked
	if w.Resumed.After(since) {
		since = w.Resumed
	}
	if since.IsZero() {
		// a new watch waits for its first tick too
		next := s.Schedule(w).Next(w.Added)
		if next.IsZero() {
			return next
		}
		return next.Add(jitter(w.Provider, next, s.Options.Jitter))
	}
	ticks := 1
	if b := s.Options.Backoff; b > 0 && w.Failures >= b {
		ticks <<= min(w.Failures-b+1, maxBackoff)
	}
	sched := s.Schedule(w)
	next := since
	for range ticks {
		next = sched.Next(next)
	}
	if next.IsZero() {
		return next
	}
	return next.Add(jitter(w.Provider, next, s.Options.Jitter))
}

// jitter is the same for a provider's watches at a tick, so they're checked together.
func jitter(provider string, tick time.Time, max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	h := fnv.New64a()
	fmt.Fprintf(h, "%s/%d", provider, tick.Unix())
	return time.Duration(h.Sum64()%uint64(max/time.Second+1)) * time.Second
}

// NextGapCheck is when the watch's gap filling is due: right away if it never ran, else
// the first tick of watch.gap_schedule after the last one (plus the jitter); zero when
// the watch has no gap filling or watch.gap_schedule is off.
func (s *Service) NextGapCheck(w storage.Watch) time.Time {
	if !w.Gaps || s.Options.GapSchedule.IsZero() {
		return time.Time{}
	}
	since := w.LastGapCheck
	if since.IsZero() {
		since = w.Added
	}
	next := s.Options.GapSchedule.Next(since)
	return next.Add(jitter(w.Provider, next, s.Options.Jitter))
}

// NextCheckSince is NextCheck, or the first tick after since when NextCheck is before it:
// a scheduler that started at since doesn't catch up on ticks missed before (e.g. while
// mangal was down), the watch waits for its next tick.
func (s *Service) NextCheckSince(w storage.Watch, since time.Time) time.Time {
	return s.notBefore(s.NextCheck(w), s.Schedule(w), w.Provider, since)
}

// ServeLock is the data db lock mangal serve holds while it runs, taken over after
// ServeLockStale without a heartbeat.
const (
	ServeLock      = "serve"
	ServeLockStale = 2 * time.Minute
)

// scheduledSince is when the running mangal serve started, now when none runs: what
// NextScheduled counts from.
func (s *Service) scheduledSince() time.Time {
	if h, ok, err := s.Watches.Holder(ServeLock, ServeLockStale); err == nil && ok {
		return h.Acquired
	}
	return s.now()
}

// NextGapCheckSince is NextGapCheck as NextCheckSince is NextCheck.
func (s *Service) NextGapCheckSince(w storage.Watch, since time.Time) time.Time {
	return s.notBefore(s.NextGapCheck(w), s.Options.GapSchedule, w.Provider, since)
}

func (s *Service) notBefore(next time.Time, sched cron.Schedule, provider string, since time.Time) time.Time {
	if next.IsZero() || !next.Before(since) {
		return next
	}
	tick := sched.Next(since)
	if tick.IsZero() {
		return tick
	}
	return tick.Add(jitter(provider, tick, s.Options.Jitter))
}

// GapsDue is true when the watch's gap filling is due (see NextGapCheck).
func (s *Service) GapsDue(w storage.Watch, now time.Time) bool {
	next := s.NextGapCheck(w)
	return !next.IsZero() && !next.After(now)
}

// Due are the enabled watches due at now, by next check.
func (s *Service) Due(ctx context.Context, now time.Time) ([]Watch, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	var due []Watch
	for _, w := range all {
		if w.Scheduled() && !w.NextCheck.After(now) {
			due = append(due, w)
		}
	}
	slices.SortStableFunc(due, func(a, b Watch) int { return a.NextCheck.Compare(b.NextCheck) })
	return due, nil
}

// Stale are the enabled watches without a new chapter for since (counted from when they
// were added if they never had one), the longest first.
func (s *Service) Stale(ctx context.Context, since time.Duration) ([]Watch, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	cutoff := s.now().Add(-since)
	last := func(w Watch) time.Time {
		if w.LastNewAt.IsZero() {
			return w.Added
		}
		return w.LastNewAt
	}
	var stale []Watch
	for _, w := range all {
		if w.Enabled && last(w).Before(cutoff) {
			stale = append(stale, w)
		}
	}
	slices.SortStableFunc(stale, func(a, b Watch) int { return last(a).Compare(last(b)) })
	return stale, nil
}

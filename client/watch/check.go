package watch

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/storage"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// CheckOptions are how to check.
type CheckOptions struct {
	// Kind of the check run, manual if empty.
	Kind string
	// DryRun only says what would be downloaded: nothing is downloaded, recorded or notified.
	DryRun bool
	// Gaps fills the gaps of every watch checked, otherwise only of the ones whose gap
	// filling is due (Service.GapsDue).
	Gaps bool
	// OnEvent is told the progress, may be nil. It's never called concurrently.
	OnEvent func(Event)
	// OnRun is told the check run once it's recorded (it got the lock), may be nil. Not
	// called on a dry run.
	OnRun func(storage.CheckRun)
}

// EventKind is what an Event is about.
type EventKind int

const (
	// EventStart is a watch about to be checked.
	EventStart EventKind = iota
	// EventChapter is the progress of a chapter download (Chapter is set).
	EventChapter
	// EventDone is a watch checked (Result is set).
	EventDone
)

// Event is the progress of Check.
type Event struct {
	Kind    EventKind
	Watch   storage.Watch
	Chapter *client.Event
	Result  *Result
}

// Result is the check of a watch: the recorded run (Chapters are the downloaded ones, or
// the ones that would be on a dry run) and what isn't recorded.
type Result struct {
	storage.WatchRun
	// Gaps is true when the gaps were filled too.
	Gaps bool `json:"gaps,omitempty"`
	// Unavailable is how many chapters the provider can't download (skipped).
	Unavailable int `json:"unavailable,omitempty"`
	// Failed is how many chapters failed to download.
	Failed int `json:"failed,omitempty"`
}

// Newest is the highest chapter number of the result, nil if none.
func (r Result) Newest() *float32 {
	var newest *float32
	for _, ch := range r.Chapters {
		if newest == nil || ch.Number > *newest {
			n := ch.Number
			newest = &n
		}
	}
	return newest
}

// Lowest is the lowest chapter number of the result, nil if none.
func (r Result) Lowest() *float32 {
	var lowest *float32
	for _, ch := range r.Chapters {
		if lowest == nil || ch.Number < *lowest {
			n := ch.Number
			lowest = &n
		}
	}
	return lowest
}

// Report is a Check: the run (ID 0 on a dry run) and the result of each watch.
type Report struct {
	storage.CheckRun
	DryRun  bool     `json:"dry_run,omitempty"`
	Results []Result `json:"results"`
}

// Check checks the watches with the ids, every enabled one if none (paused ones only when
// given). Providers are checked in parallel (watch.parallel_providers), the mangas of one
// provider one at a time; a failing one doesn't stop the others. Only one check runs at a
// time across processes (ErrBusy), dry runs aside.
//
// A cancelled ctx stops it: the chapter being downloaded is finished or dropped, what was
// done is recorded, the watches not checked yet are skipped.
func (s *Service) Check(ctx context.Context, ids []int64, opts CheckOptions) (Report, error) {
	if opts.Kind == "" {
		opts.Kind = KindManual
	}
	watches, err := s.toCheck(ids)
	if err != nil {
		return Report{}, err
	}

	if !opts.DryRun {
		unlock, err := s.lock(ctx)
		if err != nil {
			return Report{}, err
		}
		defer unlock()
	}

	report := Report{CheckRun: storage.CheckRun{Kind: opts.Kind, Started: s.now()}, DryRun: opts.DryRun}
	if !opts.DryRun {
		if report.CheckRun, err = s.Watches.StartRun(report.CheckRun); err != nil {
			return Report{}, err
		}
		if opts.OnRun != nil {
			opts.OnRun(report.CheckRun)
		}
	}

	var eventMu sync.Mutex
	onEvent := func(e Event) {
		if opts.OnEvent == nil {
			return
		}
		eventMu.Lock()
		defer eventMu.Unlock()
		opts.OnEvent(e)
	}

	// by provider, in the order of the watches
	var providers []string
	byProvider := map[string][]int{}
	for i, w := range watches {
		if _, ok := byProvider[w.Provider]; !ok {
			providers = append(providers, w.Provider)
		}
		byProvider[w.Provider] = append(byProvider[w.Provider], i)
	}

	report.Results = make([]Result, len(watches))
	sem := make(chan struct{}, max(1, s.Options.ParallelProviders))
	var wg sync.WaitGroup
	for _, provider := range providers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
			}
			s.checkProvider(ctx, provider, watches, byProvider[provider], &report, opts, onEvent)
		}()
	}
	wg.Wait()

	report.Finished = s.now()
	for _, r := range report.Results {
		if r.Status == storage.StatusSkipped {
			continue
		}
		report.Watches++
		report.NewChapters += len(r.Chapters)
		if r.Status == storage.StatusError {
			report.Errors++
		}
	}
	if !opts.DryRun {
		if err := s.Watches.FinishRun(report.CheckRun); err != nil {
			return report, err
		}
	}
	return report, nil
}

// toCheck are the watches with the ids, or every enabled one.
func (s *Service) toCheck(ids []int64) ([]storage.Watch, error) {
	if len(ids) == 0 {
		all, err := s.Watches.List()
		if err != nil {
			return nil, err
		}
		return slices.DeleteFunc(all, func(w storage.Watch) bool { return !w.Enabled }), nil
	}
	var watches []storage.Watch
	for _, id := range ids {
		w, err := s.Watches.Get(id)
		if err != nil {
			return nil, err
		}
		watches = append(watches, w)
	}
	return watches, nil
}

// lock takes the check lock and keeps it alive until unlocked.
func (s *Service) lock(ctx context.Context) (unlock func(), err error) {
	stale := s.Options.LockStale
	if stale <= 0 {
		stale = 10 * time.Minute
	}
	l, err := s.Watches.Lock(lockName, stale)
	if errors.Is(err, storage.ErrLocked) {
		return nil, fmt.Errorf("%w: %w", ErrBusy, err)
	}
	if err != nil {
		return nil, err
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(stale / 5)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := l.Heartbeat(); err != nil {
					s.warn(err)
				}
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
		if err := l.Unlock(); err != nil {
			s.warn(err)
		}
	}, nil
}

func (s *Service) warn(err error) {
	if s.OnError != nil {
		s.OnError(err)
	}
}

// checkProvider checks the watches of one provider, one at a time.
func (s *Service) checkProvider(ctx context.Context, provider string, watches []storage.Watch, indexes []int, report *Report, opts CheckOptions, onEvent func(Event)) {
	var src Source
	var srcErr error
	for _, i := range indexes {
		w := watches[i]
		if ctx.Err() != nil {
			report.Results[i] = s.skipped(w, "cancelled")
			continue
		}
		onEvent(Event{Kind: EventStart, Watch: w})
		if src == nil && srcErr == nil {
			src, srcErr = s.Source(ctx, provider)
		}
		var r Result
		if srcErr != nil {
			r = s.failed(w, s.now(), fmt.Errorf("load %s: %w", provider, srcErr))
		} else {
			gaps := opts.Gaps || s.GapsDue(w, s.now())
			r = s.checkOne(ctx, src, w, gaps, opts.DryRun, func(e client.Event) {
				onEvent(Event{Kind: EventChapter, Watch: w, Chapter: &e})
			})
		}
		if ctx.Err() != nil && r.Status == storage.StatusError && len(r.Chapters) == 0 {
			// cancelled before it got anywhere, not its fault
			r = s.skipped(w, "cancelled")
		}
		r.CheckRun = report.ID
		if !opts.DryRun {
			s.finish(ctx, w, &r)
		}
		report.Results[i] = r
		onEvent(Event{Kind: EventDone, Watch: w, Result: &r})
	}
}

func (s *Service) skipped(w storage.Watch, reason string) Result {
	now := s.now()
	return Result{WatchRun: storage.WatchRun{Watch: w.ID, Provider: w.Provider, MangaID: w.MangaID, Title: w.Title,
		Status: storage.StatusSkipped, Error: reason, Started: now, Finished: now}}
}

func (s *Service) failed(w storage.Watch, started time.Time, err error) Result {
	return Result{WatchRun: storage.WatchRun{Watch: w.ID, Provider: w.Provider, MangaID: w.MangaID, Title: w.Title,
		Status: storage.StatusError, Error: err.Error(), Started: started, Finished: s.now()}}
}

// checkOne checks a watch: gets the manga and its chapters, picks the ones to download and
// downloads them (unless dryRun). Nothing is saved here.
func (s *Service) checkOne(ctx context.Context, src Source, w storage.Watch, gaps, dryRun bool, onChapter func(client.Event)) Result {
	started := s.now()
	manga, err := s.manga(ctx, src, w)
	if err != nil {
		return s.failed(w, started, err)
	}
	if manga.Info().Title != "" {
		w.Title = manga.Info().Title
	}
	if w.AnilistID != 0 {
		if err := src.BindAnilist(ctx, manga, w.AnilistID); err != nil {
			return s.failed(w, started, err)
		}
	}
	chapters, err := src.Chapters(ctx, manga)
	if err != nil {
		return s.failed(w, started, err)
	}

	libraryManga := w.LibraryManga
	if libraryManga == 0 {
		m, found, err := s.Library.MangaFor(src.Info(), manga, s.Options.ProviderDirs)
		if err != nil {
			return s.failed(w, started, err)
		}
		if found {
			libraryManga = m.ID
		}
	}
	have := map[float32]bool{}
	if libraryManga != 0 {
		if have, err = s.Watches.LibraryNumbers(libraryManga); err != nil {
			return s.failed(w, started, err)
		}
	}

	r := Result{Gaps: gaps, WatchRun: storage.WatchRun{Watch: w.ID, Provider: w.Provider, MangaID: w.MangaID, Title: w.Title, Started: started}}
	r.From, _ = s.lastDownloaded(w, 0)
	if last := highest(have); last != nil && (r.From == nil || *last > *r.From) {
		r.From = last
	}
	picked, unavailable := Pick(chapters, have, highest(have), w.From, gaps)
	r.Unavailable = unavailable

	if dryRun {
		for _, ch := range picked {
			r.Chapters = append(r.Chapters, runChapter(ch, ""))
		}
		r.Status, r.Finished = status(len(r.Chapters), ""), s.now()
		return r
	}

	var errs []string
	results := src.Download(ctx, picked, w.AnilistID != 0, onChapter)
	for _, res := range results {
		switch {
		case res.Err != nil:
			r.Failed++
			errs = append(errs, fmt.Sprintf("chapter %s: %s", stringutil.FormatFloa32(res.Chapter.Info().Number), res.Err))
		case res.Unavailable != "":
			r.Unavailable++
		case res.Down != nil && res.Down.Skipped == "":
			r.Chapters = append(r.Chapters, runChapter(res.Chapter, res.Down.Path()))
		}
	}
	r.Error = strings.Join(errs, "; ")
	r.Status, r.Finished = status(len(r.Chapters), r.Error), s.now()
	return r
}

func status(downloaded int, err string) string {
	switch {
	case downloaded > 0:
		return storage.StatusNew
	case err != "":
		return storage.StatusError
	default:
		return storage.StatusNone
	}
}

func runChapter(ch mangadata.Chapter, path string) storage.RunChapter {
	info := ch.Info()
	rc := storage.RunChapter{Number: info.Number, Title: info.Title, URL: info.URL, Path: path}
	if v := ch.Volume(); v != nil {
		rc.Volume = v.Info().Number
	}
	return rc
}

// manga gets the watch's manga by its id, or (providers that can't) by searching its
// title for the id.
func (s *Service) manga(ctx context.Context, src Source, w storage.Watch) (mangadata.Manga, error) {
	manga, found, err := src.MangaByID(ctx, w.MangaID)
	if errors.Is(err, libmangal.ErrMangaByIDUnsupported) {
		mangas, err := src.SearchMangas(ctx, w.Title)
		if err != nil {
			return nil, fmt.Errorf("search %q: %w", w.Title, err)
		}
		for _, m := range mangas {
			if m.Info().ID == w.MangaID {
				return m, nil
			}
		}
		return nil, fmt.Errorf("manga %q not found in a search of %q", w.MangaID, w.Title)
	}
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("manga %q not found", w.MangaID)
	}
	return manga, nil
}

// Pick are the chapters to download given the chapter numbers the library has: the ones
// from the last downloaded number up (a .5 published after it too) the library doesn't
// have, with gaps also the missing ones below it. Chapters up to from (if not nil) count
// as done. Without a last and from, every chapter. One chapter per number, the first;
// unavailable ones aren't picked, only counted.
func Pick(chapters []mangadata.Chapter, have map[float32]bool, last, from *float32, gaps bool) (picked []mangadata.Chapter, unavailable int) {
	seen := map[float32]bool{}
	for _, ch := range chapters {
		info := ch.Info()
		n := info.Number
		if seen[n] || have[n] {
			continue
		}
		seen[n] = true
		if from != nil && n <= *from {
			continue
		}
		if last != nil && n < *last && !gaps {
			continue
		}
		if info.Unavailable != "" {
			unavailable++
			continue
		}
		picked = append(picked, ch)
	}
	return picked, unavailable
}

// finish saves the watch after its check and records the run, then notifies.
func (s *Service) finish(ctx context.Context, w storage.Watch, r *Result) {
	if r.Status == storage.StatusSkipped {
		if _, err := s.Watches.RecordWatchRun(r.WatchRun); err != nil {
			s.warn(err)
		}
		return
	}
	w.Title = r.Title
	w.LastChecked = r.Finished
	if r.Gaps {
		w.LastGapCheck = r.Finished
	}
	if newest := r.Newest(); newest != nil {
		w.LastNew, w.LastNewAt = newest, r.Finished
	}
	if r.Status == storage.StatusError {
		w.Failures++
	} else {
		w.Failures = 0
	}
	w.LastError = r.Error
	if w.LibraryManga == 0 {
		if m, found, err := s.Library.MangaBySource(w.Provider, w.MangaID); err != nil {
			s.warn(err)
		} else if found {
			w.LibraryManga = m.ID
		}
	}
	if err := s.Watches.Save(w); err != nil {
		s.warn(err)
	}
	recorded, err := s.Watches.RecordWatchRun(r.WatchRun)
	if err != nil {
		s.warn(err)
	} else {
		r.ID = recorded.ID
	}
	s.notify(ctx, *r)
}

func (s *Service) notify(ctx context.Context, r Result) {
	if s.Notifier == nil {
		return
	}
	mode := s.Options.Notify
	// a cancelled check still tells what it got
	ctx = context.WithoutCancel(ctx)
	if len(r.Chapters) > 0 && (mode == NotifyNew || mode == NotifyBoth) {
		if err := s.Notifier.New(ctx, r); err != nil {
			s.warn(err)
		}
	}
	if r.Error != "" && (mode == NotifyErrors || mode == NotifyBoth) {
		if err := s.Notifier.Error(ctx, r); err != nil {
			s.warn(err)
		}
	}
}

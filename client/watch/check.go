package watch

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/libmangal/progress"
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
	// OnLive is told the progress as LiveEvents, may be nil. It's never called
	// concurrently. A Remote check streams them from the server (not on a dry run).
	OnLive func(LiveEvent)
	// OnRun is told the check run once it's recorded (it got the locks), may be nil. Not
	// called on a dry run.
	OnRun func(storage.CheckRun)
	// OnBusy is told the providers skipped because another run has them (before OnRun),
	// may be nil.
	OnBusy func(providers []string)
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
	// EventFound is a watch's chapters listed, before downloading (Found is set).
	EventFound
	// EventBusy is the provider, under the id it has now (the watch has an old one or an
	// alias), locked by a download: the check waits for it (Busy is set).
	EventBusy
	// EventStatus is what the provider is doing (libmangal's progress, e.g. a mirror down,
	// a rate limit, a page of results), Status is set.
	EventStatus
)

// Event is the progress of Check.
type Event struct {
	Kind    EventKind
	Watch   storage.Watch
	Chapter *client.Event
	Result  *Result
	Found   *Found
	Busy    *client.ProviderBusy
	Status  *progress.Status
}

// Result is the check of a watch: the recorded run (Chapters are the downloaded ones, or
// the ones that would be on a dry run) and what isn't recorded.
type Result struct {
	storage.WatchRun
	// Gaps is true when the gaps were filled too.
	Gaps bool `json:"gaps,omitempty"`
	// Unavailable is how many chapters the provider can't download (skipped).
	Unavailable int `json:"unavailable,omitempty"`
	// Failed is how many chapters failed to download, Retry their numbers (the next check
	// tries them again).
	Failed int       `json:"failed,omitempty"`
	Retry  []float32 `json:"retry,omitempty"`
	// downloaded is true when the check got to download: the watch's Retry is replaced.
	downloaded bool
	// ids are the manga's metadata ids, to find its library manga after the check.
	ids []metadata.ID
	// Relink is set when the provider doesn't have the manga (ErrMangaNotFound): the
	// watch needs a re-link (not counted as a failure, not notified).
	Relink string `json:"relink,omitempty"`
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
	// Busy are the providers another run had: their watches were skipped.
	Busy []string `json:"busy,omitempty"`
}

// Check checks the watches with the ids, every enabled one if none (paused ones only when
// given). Providers are checked in parallel (watch.parallel_providers), the mangas of one
// provider one at a time; a failing one doesn't stop the others.
//
// A provider is checked by one run at a time across processes (its lock): the watches of
// the providers another run has are skipped ("provider busy", not recorded, Report.Busy);
// when every provider is busy it's ErrBusy. Dry runs take no lock.
//
// A cancelled ctx stops it: the chapter being downloaded is finished or dropped, what was
// done is recorded, the watches not checked yet are skipped.
func (s *Service) Check(ctx context.Context, ids []int64, opts CheckOptions) (Report, error) {
	if opts.Kind == "" {
		opts.Kind = KindManual
	}
	watches, err := s.toCheck(ctx, ids)
	if err != nil {
		return Report{}, err
	}

	// by provider (an old id's watches with the current id's: one lock, one at a time),
	// in the order of the watches
	var providers []string
	byProvider := map[string][]int{}
	info := s.providerInfo()
	for i, w := range watches {
		p := info(w.Provider).ID
		if _, ok := byProvider[p]; !ok {
			providers = append(providers, p)
		}
		byProvider[p] = append(byProvider[p], i)
	}

	report := Report{CheckRun: storage.CheckRun{Kind: opts.Kind, Started: s.now()}, DryRun: opts.DryRun}
	report.Results = make([]Result, len(watches))
	var locks *providerLocks
	if !opts.DryRun {
		var busy []string
		locks, busy, err = s.lockProviders(providers)
		if err != nil {
			return Report{}, err
		}
		defer locks.unlockAll()
		if len(busy) > 0 {
			report.Busy = busy
			providers = slices.DeleteFunc(providers, func(p string) bool { return slices.Contains(busy, p) })
			for _, p := range busy {
				for _, i := range byProvider[p] {
					report.Results[i] = s.skipped(watches[i], "provider busy")
				}
			}
			if opts.OnBusy != nil {
				opts.OnBusy(busy)
			}
		}
		if report.CheckRun, err = s.Watches.StartRun(report.CheckRun); err != nil {
			return Report{}, err
		}
		if opts.OnRun != nil {
			opts.OnRun(report.CheckRun)
		}
	}

	hub := s.Events
	if opts.DryRun {
		hub = nil
	}
	if hub != nil {
		n := 0
		for _, p := range providers {
			hub.begin(p, report.ID, locks.since(p))
			n += len(byProvider[p])
		}
		hub.publish(LiveEvent{Run: report.ID, Kind: LiveRunStart, Message: runStartMessage(report.CheckRun, providers, n, report.Busy)})
	}

	var eventMu sync.Mutex
	onEvent := func(e Event) {
		le, live := liveEvent(report.ID, e)
		if hub != nil && live {
			hub.publish(le)
		}
		eventMu.Lock()
		defer eventMu.Unlock()
		if opts.OnLive != nil && live {
			opts.OnLive(le)
		}
		if opts.OnEvent != nil {
			opts.OnEvent(e)
		}
	}

	sem := make(chan struct{}, max(1, s.Options.ParallelProviders))
	var wg sync.WaitGroup
	for _, provider := range providers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// the provider is free for other runs as soon as it's done
			defer locks.unlock(provider)
			if hub != nil {
				defer hub.end(provider)
			}
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
			}
			if hub != nil {
				hub.publish(LiveEvent{Run: report.ID, Kind: LiveProviderStart, Provider: provider,
					Message: "checking " + stringutil.Quantify(len(byProvider[provider]), "watch", "watches")})
			}
			s.checkProvider(ctx, provider, watches, byProvider[provider], &report, opts, onEvent)
			if hub != nil {
				hub.publish(LiveEvent{Run: report.ID, Kind: LiveProviderDone, Provider: provider, Message: "done"})
			}
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
	if hub != nil {
		took := report.Finished.Sub(report.Started).Round(time.Second)
		hub.publish(LiveEvent{Run: report.ID, Kind: LiveRunDone, Message: fmt.Sprintf("%s check %d done in %s: %s, %s, %s", report.Kind, report.ID, took,
			stringutil.Quantify(report.Watches, "watch", "watches"), stringutil.Quantify(report.NewChapters, "new chapter", "new chapters"),
			stringutil.Quantify(report.Errors, "error", "errors"))})
	}
	if !opts.DryRun {
		if err := s.Watches.FinishRun(report.CheckRun); err != nil {
			return report, err
		}
	}
	return report, nil
}

// toCheck are the watches with the ids, or every scheduled one (Watch.Scheduled).
func (s *Service) toCheck(ctx context.Context, ids []int64) ([]storage.Watch, error) {
	if len(ids) == 0 {
		all, err := s.List(ctx)
		if err != nil {
			return nil, err
		}
		var watches []storage.Watch
		for _, w := range all {
			if w.Scheduled() {
				watches = append(watches, w.Watch)
			}
		}
		return watches, nil
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

// lockPrefix starts the data db lock of each provider, shared with the downloads (see
// client.ProviderLockName): a check has a provider's lock for its whole run of it.
const lockPrefix = client.ProviderLockPrefix

func lockName(provider string) string { return client.ProviderLockName(provider) }

func (s *Service) lockStale() time.Duration {
	if s.Options.LockStale <= 0 {
		return 10 * time.Minute
	}
	return s.Options.LockStale
}

// providerLocks are the locks a check holds, kept alive until unlocked.
type providerLocks struct {
	s       *Service
	mu      sync.Mutex
	held    map[string]*storage.Lock
	stop    chan struct{}
	stopped chan struct{}
}

// lockProviders takes the providers' locks, busy are the ones held by others. Every one
// busy is ErrBusy.
func (s *Service) lockProviders(providers []string) (*providerLocks, []string, error) {
	stale := s.lockStale()
	l := &providerLocks{s: s, held: map[string]*storage.Lock{}, stop: make(chan struct{}), stopped: make(chan struct{})}
	var busy []string
	var busyErr error
	for _, p := range providers {
		lock, err := s.Watches.LockAs(lockName(p), client.LockForWatch, stale)
		if errors.Is(err, storage.ErrLocked) {
			busy = append(busy, p)
			busyErr = cmp.Or(busyErr, err)
			continue
		}
		if err != nil {
			close(l.stopped)
			l.unlockAll()
			return nil, nil, err
		}
		l.held[p] = lock
	}
	if len(providers) > 0 && len(busy) == len(providers) {
		close(l.stopped)
		return nil, nil, fmt.Errorf("%w (%s): %w", ErrBusy, strings.Join(busy, ", "), busyErr)
	}
	go l.beat(stale / 5)
	return l, busy, nil
}

func (l *providerLocks) beat(every time.Duration) {
	defer close(l.stopped)
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-ticker.C:
			l.mu.Lock()
			for _, lock := range l.held {
				if err := lock.Heartbeat(); err != nil {
					l.s.warn(err)
				}
			}
			l.mu.Unlock()
		}
	}
}

// since is when the provider's lock was taken.
func (l *providerLocks) since(provider string) time.Time {
	if l == nil {
		return time.Time{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if lock, ok := l.held[provider]; ok {
		return lock.Acquired
	}
	return time.Time{}
}

func (l *providerLocks) unlock(provider string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if lock, ok := l.held[provider]; ok {
		delete(l.held, provider)
		if err := lock.Unlock(); err != nil {
			l.s.warn(err)
		}
	}
}

// unlockAll releases what's still held and stops the heartbeat.
func (l *providerLocks) unlockAll() {
	if l == nil {
		return
	}
	select {
	case <-l.stop:
	default:
		close(l.stop)
	}
	<-l.stopped
	l.mu.Lock()
	defer l.mu.Unlock()
	for p, lock := range l.held {
		delete(l.held, p)
		if err := lock.Unlock(); err != nil {
			l.s.warn(err)
		}
	}
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
	missing := !s.installed()(provider)
	// the check has the provider's lock: its downloads don't wait for it
	ctx = client.HoldingProvider(ctx, provider)
	for _, i := range indexes {
		w := watches[i]
		if ctx.Err() != nil {
			report.Results[i] = s.skipped(w, "cancelled")
			continue
		}
		onEvent(Event{Kind: EventStart, Watch: w})
		status := func(ctx context.Context) context.Context {
			return progress.WithReporter(ctx, func(st progress.Status) {
				onEvent(Event{Kind: EventStatus, Watch: w, Status: &st})
			})
		}
		if src == nil && srcErr == nil && !missing {
			src, srcErr = s.Source(status(ctx), provider)
			if srcErr == nil && src.Info().ID != provider && !opts.DryRun {
				var unlock func()
				ctx, unlock, srcErr = client.LockProvider(ctx, s.Watches, src.Info().ID, client.LockForWatch, func(b client.ProviderBusy) {
					onEvent(Event{Kind: EventBusy, Watch: w, Busy: &b})
				})
				if srcErr == nil {
					defer unlock()
				}
			}
		}
		var r Result
		if missing || errors.Is(srcErr, client.ErrProviderNotFound) {
			// not a failure: the provider was removed, it may come back
			r = s.skipped(w, "provider not installed")
		} else if srcErr != nil {
			r = s.failed(w, s.now(), fmt.Errorf("load %s: %w", provider, srcErr))
		} else {
			gaps := opts.Gaps || s.GapsDue(w, s.now())
			r = s.checkOne(status(ctx), src, w, gaps, opts.DryRun, func(f Found) {
				onEvent(Event{Kind: EventFound, Watch: w, Found: &f})
			}, func(e client.Event) {
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
func (s *Service) checkOne(ctx context.Context, src Source, w storage.Watch, gaps, dryRun bool, onFound func(Found), onChapter func(client.Event)) Result {
	if w.LibraryManga == 0 {
		// its library manga in another provider's directory: its downloads would go to a
		// folder of their own, and the chapters it has there wouldn't count
		match, err := s.matchLibrary(src.Info(), w, nil)
		if err != nil {
			return s.failed(w, s.now(), err)
		}
		if match.Elsewhere {
			return s.skipped(w, ElsewhereReason(match.Manga, w.Provider))
		}
	}
	started := s.now()
	manga, err := s.manga(ctx, src, w)
	if err != nil {
		r := s.failed(w, started, err)
		if errors.Is(err, ErrMangaNotFound) {
			r.Relink = fmt.Sprintf("manga %q not found on %s", w.MangaID, src.Info().ID)
		}
		return r
	}
	if manga.Info().Title != "" {
		w.Title = manga.Info().Title
	}
	chapters, err := src.Chapters(ctx, manga)
	if err != nil {
		return s.failed(w, started, err)
	}

	libraryManga := w.LibraryManga
	ids := storage.MangaMetadataIDs(manga)
	if libraryManga == 0 {
		match, err := s.matchLibrary(src.Info(), w, ids)
		if err != nil {
			return s.failed(w, started, err)
		}
		if match.Linked {
			libraryManga = match.Manga.ID
		}
	}
	have := map[float32]bool{}
	if libraryManga != 0 {
		if have, err = s.Watches.LibraryNumbers(libraryManga); err != nil {
			return s.failed(w, started, err)
		}
	}

	r := Result{Gaps: gaps, WatchRun: storage.WatchRun{Watch: w.ID, Provider: w.Provider, MangaID: w.MangaID, Title: w.Title, Started: started}, ids: ids}
	r.From, _ = s.lastDownloaded(w, 0)
	if last := highest(have); last != nil && (r.From == nil || *last > *r.From) {
		r.From = last
	}
	retry := map[float32]bool{}
	for _, n := range w.Retry {
		retry[n] = true
	}
	skip := map[float32]bool{}
	for _, n := range w.Skip {
		skip[n] = true
	}
	picked, unavailable, skipped := Pick(chapters, have, highest(have), w.From, gaps, retry, skip)
	r.Unavailable, r.Skipped = unavailable, skipped
	if onFound != nil {
		found := Found{Chapters: len(chapters), From: r.From, Unavailable: unavailable, Skipped: skipped}
		for _, ch := range picked {
			found.Picked = append(found.Picked, ch.Info().Number)
		}
		onFound(found)
	}

	if dryRun {
		for _, ch := range picked {
			r.Chapters = append(r.Chapters, runChapter(ch, ""))
		}
		r.Status, r.Finished = status(len(r.Chapters), ""), s.now()
		return r
	}

	var errs []string
	results := src.Download(ctx, picked, onChapter)
	r.downloaded = true
	for _, res := range results {
		switch {
		case res.Err != nil:
			r.Failed++
			r.Retry = append(r.Retry, res.Chapter.Info().Number)
			errs = append(errs, fmt.Sprintf("chapter %s: %s", stringutil.FormatFloa32(res.Chapter.Info().Number), res.Err))
		case res.Unavailable != "":
			r.Unavailable++
		case res.Down == nil:
			// not got to (cancelled): picked again next time, wherever it is by then
			r.Retry = append(r.Retry, res.Chapter.Info().Number)
		case res.Down.Skipped == "":
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

// manga gets the watch's manga (FindManga).
func (s *Service) manga(ctx context.Context, src Source, w storage.Watch) (mangadata.Manga, error) {
	return FindManga(ctx, src, w.MangaID, w.Title)
}

// FindManga gets the provider's manga by its id, or (providers that can't) by searching
// its title for the id. ErrMangaNotFound when the provider doesn't have it.
func FindManga(ctx context.Context, src Source, mangaID, title string) (mangadata.Manga, error) {
	manga, found, err := src.MangaByID(ctx, mangaID)
	if errors.Is(err, libmangal.ErrMangaByIDUnsupported) {
		mangas, err := src.SearchMangas(ctx, title)
		if err != nil {
			return nil, fmt.Errorf("search %q: %w", title, err)
		}
		for _, m := range mangas {
			if m.Info().ID == mangaID {
				return m, nil
			}
		}
		return nil, fmt.Errorf("manga %q in a search of %q: %w", mangaID, title, ErrMangaNotFound)
	}
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("manga %q: %w", mangaID, ErrMangaNotFound)
	}
	return manga, nil
}

// NewChapters are the chapter numbers of the provider's manga newer than the highest one
// the library has (have), as a check without gaps would pick them (Pick): unavailable
// ones left out.
func NewChapters(ctx context.Context, src Source, mangaID, title string, have []float32) ([]float32, error) {
	manga, err := FindManga(ctx, src, mangaID, title)
	if err != nil {
		return nil, err
	}
	chapters, err := src.Chapters(ctx, manga)
	if err != nil {
		return nil, err
	}
	haveSet := map[float32]bool{}
	for _, n := range have {
		haveSet[n] = true
	}
	picked, _, _ := Pick(chapters, haveSet, highest(haveSet), nil, false, nil, nil)
	numbers := make([]float32, len(picked))
	for i, ch := range picked {
		numbers[i] = ch.Info().Number
	}
	return numbers, nil
}

// Pick are the chapters to download given the chapter numbers the library has: the ones
// from the last downloaded number up (a .5 published after it too) the library doesn't
// have, with gaps also the missing ones below it, and the ones in retry (a check failed to
// download them) wherever they are. Chapters up to from (if not nil) count as done. Without a last and from, every chapter. One chapter per number, the first;
// unavailable ones aren't picked, only counted. The numbers in skip are never picked
// (retry or not): skipped are the ones that would have been.
func Pick(chapters []mangadata.Chapter, have map[float32]bool, last, from *float32, gaps bool, retry, skip map[float32]bool) (picked []mangadata.Chapter, unavailable int, skipped []float32) {
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
		if last != nil && n < *last && !gaps && !retry[n] {
			continue
		}
		if skip[n] {
			skipped = append(skipped, n)
			continue
		}
		if info.Unavailable != "" {
			unavailable++
			continue
		}
		picked = append(picked, ch)
	}
	return picked, unavailable, skipped
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
	switch {
	case r.Relink != "":
		// the manga id is wrong: no backoff, it waits for a re-link
		w.NeedsRelink, w.RelinkReason = true, r.Relink
	case r.Status == storage.StatusError:
		w.Failures++
	default:
		w.Failures = 0
		w.NeedsRelink, w.RelinkReason = false, ""
	}
	w.LastError = r.Error
	if r.downloaded {
		w.Retry = r.Retry
	}
	if w.LibraryManga == 0 {
		// after the downloads, which made the manga its source
		if match, err := s.matchLibrary(s.providerInfo()(w.Provider), w, r.ids); err != nil {
			s.warn(err)
		} else if match.Linked {
			w.LibraryManga = match.Manga.ID
		}
	}
	if err := s.Watches.SaveCheck(w); err != nil {
		s.warn(err)
	} else if err := s.linkSource(w); err != nil {
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
	if r.Error != "" && r.Relink == "" && (mode == NotifyErrors || mode == NotifyBoth) {
		if err := s.Notifier.Error(ctx, r); err != nil {
			s.warn(err)
		}
	}
}

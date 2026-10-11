// Package watch keeps watched mangas up to date: each watch is one provider's manga,
// checked for chapters newer than the last one the library has (and optionally the gaps
// below it), downloaded with client.Download and recorded as check runs.
//
// Service is what the CLI, `mangal serve` and the TUI use; it never prints.
package watch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"slices"
	"strconv"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/selector"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
)

// Kinds of check runs.
const (
	KindScheduled = "scheduled"
	KindManual    = "manual"
	KindGaps      = "gaps"
	KindImport    = "import"
)

// Notify modes (watch.notify).
const (
	NotifyNew    = "new"
	NotifyErrors = "errors"
	NotifyBoth   = "both"
	NotifyNone   = "none"
)

var (
	// ErrBusy is a check of providers another check has (in this or another process).
	ErrBusy = errors.New("providers busy with another check")
	// ErrNotFound is a watch id that doesn't exist.
	ErrNotFound = storage.ErrWatchNotFound
	// ErrExists is watching a manga that's already watched.
	ErrExists = storage.ErrWatchExists
)

type (
	CheckRun   = storage.CheckRun
	WatchRun   = storage.WatchRun
	RunChapter = storage.RunChapter
	RunFilter  = storage.RunFilter
)

// Watch is a watched manga, with what's worked out from the library and the config.
type Watch struct {
	storage.Watch
	// LastDownloaded is the highest chapter number the library has of it (or From, if
	// higher), nil when there's none: the next check starts from it.
	LastDownloaded *float32 `json:"last_downloaded,omitempty"`
	// NextCheck is when it's due, see Service.NextCheck.
	NextCheck time.Time `json:"next_check"`
	// ProviderMissing is a provider that isn't installed: the checks skip it (without an
	// error) until it is again.
	ProviderMissing bool `json:"provider_missing,omitempty"`
	// Bindings are the manga's metadata bindings (source, id, default), the default first.
	Bindings []storage.Binding `json:"bindings,omitempty"`
}

// UnmarshalJSON fills AnilistID from the bindings (the API's watches, see Remote).
func (w *Watch) UnmarshalJSON(b []byte) error {
	type plain Watch
	if err := json.Unmarshal(b, (*plain)(w)); err != nil {
		return err
	}
	w.fillBindings(w.Bindings)
	return nil
}

// fillBindings sets Bindings without their provider and manga (the watch's) and AnilistID.
func (w *Watch) fillBindings(bindings []storage.Binding) {
	w.Bindings, w.AnilistID = nil, 0
	for _, b := range bindings {
		if b.Source == metadata.IDCodeAnilist {
			w.AnilistID, _ = strconv.Atoi(b.ID)
		}
		b.Provider, b.MangaID = "", ""
		w.Bindings = append(w.Bindings, b)
	}
}

// Options are the watch.* settings.
type Options struct {
	// Interval between checks of a watch without its own.
	Interval time.Duration
	// Jitter is up to how much later than its interval a watch is due.
	Jitter time.Duration
	// GapInterval is how often gap filling runs on the watches that have it, 0 never.
	GapInterval time.Duration
	// Gaps is gap filling for new watches.
	Gaps bool
	// ParallelProviders is how many providers are checked at once.
	ParallelProviders int
	// Backoff is how many failures in a row make a watch wait longer, 0 never.
	Backoff int
	// Notify is new, errors, both or none.
	Notify string
	// ProviderDirs is download.provider.create_dir: the library mangas matched by metadata
	// id must be under the provider's directory.
	ProviderDirs bool
	// LockStale is when a check lock without a heartbeat is taken over.
	LockStale time.Duration
}

// ConfigOptions are the Options from the config.
func ConfigOptions() Options {
	duration := func(s string, def time.Duration) time.Duration {
		d, err := time.ParseDuration(s)
		if err != nil {
			return def
		}
		return d
	}
	return Options{
		// a 0 interval would check all the time
		Interval:          max(time.Minute, duration(config.Watch.Interval.Get(), 4*time.Hour)),
		Jitter:            duration(config.Watch.Jitter.Get(), 0),
		GapInterval:       duration(config.Watch.GapInterval.Get(), 0),
		Gaps:              config.Watch.Gaps.Get(),
		ParallelProviders: config.Watch.ParallelProviders.Get(),
		Backoff:           config.Watch.Backoff.Get(),
		Notify:            config.Watch.Notify.Get(),
		ProviderDirs:      config.Download.Provider.CreateDir.Get(),
		LockStale:         10 * time.Minute,
	}
}

// Source is a provider as the watches use it, see ClientSource.
type Source interface {
	Info() libmangal.ProviderInfo
	// MangaByID is libmangal's Client.MangaByIDContext, its error matches
	// libmangal.ErrMangaByIDUnsupported when the provider can't.
	MangaByID(ctx context.Context, id string) (mangadata.Manga, bool, error)
	SearchMangas(ctx context.Context, query string) ([]mangadata.Manga, error)
	Chapters(ctx context.Context, manga mangadata.Manga) ([]mangadata.Chapter, error)
	// BoundMetadata is the metadata of a source's manga id (client.BoundMetadata), to check
	// a binding.
	BoundMetadata(ctx context.Context, code metadata.IDCode, id string) (metadata.Metadata, error)
	// Download downloads the chapters (client.Download, which uses the manga's default
	// metadata binding).
	Download(ctx context.Context, chapters []mangadata.Chapter, onEvent func(client.Event)) client.Results
}

// Notifier sends the notifications of the checks.
type Notifier interface {
	// New is a watch that downloaded chapters.
	New(ctx context.Context, r Result) error
	// Error is a watch whose check failed (or some of its chapters).
	Error(ctx context.Context, r Result) error
}

// Service manages the watches.
type Service struct {
	Watches storage.Watches
	Library storage.Library
	Options Options
	// Source gets the provider with the id (an old id works too).
	Source func(ctx context.Context, provider string) (Source, error)
	// Installed tells which providers are installed (old ids too); nil = every one is.
	Installed func() (func(provider string) bool, error)
	// Notifier may be nil.
	Notifier Notifier
	// Now is time.Now, swapped in tests.
	Now func() time.Time
	// OnError is told about errors that don't fail anything (a notification that couldn't
	// be sent), may be nil.
	OnError func(error)
	// Events broadcasts the progress of the checks (Subscribe), may be nil.
	Events *Hub
}

// New is the Service over mangal's databases, with the config's options and the
// providers' clients.
func New(notifier Notifier) (*Service, error) {
	st, err := cache.Storage()
	if err != nil {
		return nil, err
	}
	return &Service{
		Watches:   st.Watches(),
		Library:   st.Library(),
		Options:   ConfigOptions(),
		Source:    ClientSource,
		Installed: installedProviders,
		Notifier:  notifier,
		Now:       time.Now,
		Events:    NewHub(),
	}, nil
}

func (s *Service) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

// AddOptions say what to watch: the manga with MangaID, or the one Selector picks from a
// search of Query (a "mid:<id>" query is the id).
type AddOptions struct {
	// Offline adds the watch as given (MangaID or a "mid:" Query, Title) without asking the
	// provider, which needn't be installed (e.g. to keep the watches of a provider that's
	// gone); it's paused unless Enabled says otherwise.
	Offline bool
	Title   string
	Enabled *bool

	Provider string
	MangaID  string
	Query    string
	// Selector is a manga selector (client/selector), exact if empty.
	Selector string
	// AnilistID binds the AniList manga (the default binding), 0 leaves the bindings.
	AnilistID int
	// From treats the chapters up to it as done, nil for none.
	From *float32
	// Gaps is gap filling, nil for watch.gaps.
	Gaps *bool
	// Interval overrides watch.interval when not 0.
	Interval time.Duration
}

// Add watches a manga. A manga already watched is ErrExists.
func (s *Service) Add(ctx context.Context, opts AddOptions) (Watch, error) {
	if opts.Offline {
		return s.addOffline(opts)
	}
	src, err := s.Source(ctx, opts.Provider)
	if err != nil {
		return Watch{}, err
	}
	manga, err := s.find(ctx, src, opts)
	if err != nil {
		return Watch{}, err
	}
	info := src.Info()
	if _, found, err := s.Watches.Find(info.ID, manga.Info().ID); err != nil {
		return Watch{}, err
	} else if found {
		return Watch{}, fmt.Errorf("watch %q from %s: %w", manga.Info().Title, info.ID, ErrExists)
	}
	if opts.AnilistID != 0 {
		if _, err := src.BoundMetadata(ctx, metadata.IDCodeAnilist, strconv.Itoa(opts.AnilistID)); err != nil {
			return Watch{}, err
		}
	}
	w := storage.Watch{
		Provider: info.ID,
		MangaID:  manga.Info().ID,
		Title:    manga.Info().Title,
		Enabled:  true,
		Interval: storage.Duration(opts.Interval),
		Gaps:     s.Options.Gaps,
		From:     opts.From,
		Added:    s.now(),
	}
	if opts.Gaps != nil {
		w.Gaps = *opts.Gaps
	}
	if m, found, err := s.Library.MangaFor(info, manga, s.Options.ProviderDirs); err != nil {
		return Watch{}, err
	} else if found {
		w.LibraryManga = m.ID
	}
	if w, err = s.Watches.Add(w); err != nil {
		return Watch{}, err
	}
	if err := s.bindAnilist(w, opts.AnilistID); err != nil {
		return Watch{}, err
	}
	return s.complete(w)
}

// addOffline adds the watch as AddOptions say, the provider isn't asked.
func (s *Service) addOffline(opts AddOptions) (Watch, error) {
	id := opts.MangaID
	if id == "" {
		id, _ = selector.IDQuery(opts.Query)
	}
	if opts.Provider == "" || id == "" {
		return Watch{}, errors.New("an offline watch needs the provider and the manga id (mid:<id>)")
	}
	title := opts.Title
	if title == "" {
		title = id
	}
	if _, found, err := s.Watches.Find(opts.Provider, id); err != nil {
		return Watch{}, err
	} else if found {
		return Watch{}, fmt.Errorf("watch %q from %s: %w", title, opts.Provider, ErrExists)
	}
	w := storage.Watch{
		Provider: opts.Provider,
		MangaID:  id,
		Title:    title,
		Interval: storage.Duration(opts.Interval),
		Gaps:     s.Options.Gaps,
		From:     opts.From,
		Added:    s.now(),
	}
	if opts.Enabled != nil {
		w.Enabled = *opts.Enabled
	}
	if opts.Gaps != nil {
		w.Gaps = *opts.Gaps
	}
	w, err := s.Watches.Add(w)
	if err != nil {
		return Watch{}, err
	}
	if err := s.bindAnilist(w, opts.AnilistID); err != nil {
		return Watch{}, err
	}
	full, err := s.complete(w)
	full.ProviderMissing = err == nil && !s.installed()(w.Provider)
	return full, err
}

// installedProviders is the installed providers (old ids too), for Service.Installed.
func installedProviders() (func(string) bool, error) {
	loaders, err := manager.Loaders()
	if err != nil {
		return nil, err
	}
	return func(provider string) bool {
		_, _, ok := libmangal.FindLoader(loaders, provider)
		return ok
	}, nil
}

// installed is Installed, or every provider when there's none.
func (s *Service) installed() func(string) bool {
	if s.Installed != nil {
		if is, err := s.Installed(); err == nil {
			return is
		} else {
			s.warn(fmt.Errorf("installed providers: %w", err))
		}
	}
	return func(string) bool { return true }
}

// find is the manga AddOptions name.
func (s *Service) find(ctx context.Context, src Source, opts AddOptions) (mangadata.Manga, error) {
	id := opts.MangaID
	if id == "" {
		id, _ = selector.IDQuery(opts.Query)
	}
	if id != "" {
		manga, found, err := src.MangaByID(ctx, id)
		switch {
		case errors.Is(err, libmangal.ErrMangaByIDUnsupported) && opts.Query != "":
			// mangoprovider's searches take "mid:" queries too
			return s.pick(ctx, src, opts.Query, "id", id)
		case errors.Is(err, libmangal.ErrMangaByIDUnsupported):
			return nil, fmt.Errorf("%s can't get a manga by id, add it with a title query", src.Info().ID)
		case err != nil:
			return nil, err
		case !found:
			return nil, fmt.Errorf("no manga with id %q in %s", id, src.Info().ID)
		}
		return manga, nil
	}
	if opts.Query == "" {
		return nil, errors.New("no manga id or query")
	}
	sel := opts.Selector
	if sel == "" {
		sel = "exact"
	}
	return s.pick(ctx, src, opts.Query, sel, "")
}

// pick searches the query and picks one manga with the selector (with an id, it must be
// that manga).
func (s *Service) pick(ctx context.Context, src Source, query, sel, id string) (mangadata.Manga, error) {
	mangas, err := src.SearchMangas(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("search %q: %w", query, err)
	}
	if len(mangas) == 0 {
		return nil, fmt.Errorf("no mangas found in %s with %q", src.Info().ID, query)
	}
	indexes, err := selector.Mangas(sel, query, mangas)
	if err != nil {
		return nil, err
	}
	if len(indexes) != 1 {
		return nil, fmt.Errorf("manga selector %q picks %d mangas, it needs to pick 1", sel, len(indexes))
	}
	manga := mangas[indexes[0]]
	if id != "" && manga.Info().ID != id {
		return nil, fmt.Errorf("no manga with id %q in %s", id, src.Info().ID)
	}
	return manga, nil
}

// List lists every watch, by id.
func (s *Service) List(ctx context.Context) ([]Watch, error) {
	list, err := s.Watches.List()
	if err != nil {
		return nil, err
	}
	installed := s.installed()
	watches := make([]Watch, len(list))
	for i, w := range list {
		if watches[i], err = s.complete(w); err != nil {
			return nil, err
		}
		watches[i].ProviderMissing = !installed(w.Provider)
	}
	return watches, nil
}

// Get is the watch with the id, ErrNotFound if none.
func (s *Service) Get(ctx context.Context, id int64) (Watch, error) {
	w, err := s.Watches.Get(id)
	if err != nil {
		return Watch{}, err
	}
	full, err := s.complete(w)
	full.ProviderMissing = err == nil && !s.installed()(w.Provider)
	return full, err
}

// complete adds what's worked out to the stored watch.
// bindings are the metadata bindings, in the watches' database.
func (s *Service) bindings() storage.Bindings {
	return storage.Bindings{DB: s.Watches.DB}
}

// bindAnilist binds the watched manga to the AniList manga with the id (the default, AniList
// comes first), nothing for 0 (the shortcut of --anilist-id).
func (s *Service) bindAnilist(w storage.Watch, anilistID int) error {
	if anilistID == 0 {
		return nil
	}
	return s.bindings().Set(storage.Binding{Provider: w.Provider, MangaID: w.MangaID, Source: metadata.IDCodeAnilist,
		ID: strconv.Itoa(anilistID), Updated: s.now()})
}

func (s *Service) complete(w storage.Watch) (Watch, error) {
	full := Watch{Watch: w, NextCheck: s.NextCheck(w)}
	bindings, err := s.bindings().Get(w.Provider, w.MangaID)
	if err != nil {
		return Watch{}, err
	}
	full.fillBindings(bindings)
	last, err := s.lastDownloaded(w, w.LibraryManga)
	if err != nil {
		return Watch{}, err
	}
	full.LastDownloaded = last
	return full, nil
}

// lastDownloaded is the highest chapter number the library manga has, or the watch's From
// if higher, nil if neither.
func (s *Service) lastDownloaded(w storage.Watch, libraryManga int64) (*float32, error) {
	var last *float32
	if libraryManga != 0 {
		have, err := s.Watches.LibraryNumbers(libraryManga)
		if err != nil {
			return nil, err
		}
		last = highest(have)
	}
	if w.From != nil && (last == nil || *w.From > *last) {
		from := *w.From
		last = &from
	}
	return last, nil
}

func highest(numbers map[float32]bool) *float32 {
	var last *float32
	for n := range numbers {
		if last == nil || n > *last {
			n := n
			last = &n
		}
	}
	return last
}

// Remove stops watching, the history of its checks stays.
func (s *Service) Remove(ctx context.Context, id int64) error {
	return s.Watches.Remove(id)
}

// SetEnabled pauses (false) or resumes the watch.
func (s *Service) SetEnabled(ctx context.Context, id int64, enabled bool) (Watch, error) {
	return s.update(id, func(w *storage.Watch) { w.Enabled = enabled })
}

// UpdateOptions are the settings of a watch to change, nil keeps them.
type UpdateOptions struct {
	// Interval overrides watch.interval, 0 goes back to it.
	Interval *time.Duration
	Gaps     *bool
	// AnilistID binds an AniList manga (the default binding), 0 unbinds it.
	AnilistID *int
	// From treats the chapters up to it as done.
	From *float32
}

// Update changes the watch's settings.
func (s *Service) Update(ctx context.Context, id int64, opts UpdateOptions) (Watch, error) {
	if opts.AnilistID != nil {
		w, err := s.Watches.Get(id)
		if err != nil {
			return Watch{}, err
		}
		if *opts.AnilistID == 0 {
			if _, err := s.bindings().Remove(w.Provider, w.MangaID, metadata.IDCodeAnilist); err != nil {
				return Watch{}, err
			}
		} else if err := s.bindAnilist(w, *opts.AnilistID); err != nil {
			return Watch{}, err
		}
	}
	return s.update(id, func(w *storage.Watch) {
		if opts.Interval != nil {
			w.Interval = storage.Duration(*opts.Interval)
		}
		if opts.Gaps != nil {
			w.Gaps = *opts.Gaps
		}
		if opts.From != nil {
			w.From = opts.From
		}
	})
}

func (s *Service) update(id int64, change func(*storage.Watch)) (Watch, error) {
	w, err := s.Watches.Get(id)
	if err != nil {
		return Watch{}, err
	}
	change(&w)
	if err := s.Watches.Save(w); err != nil {
		return Watch{}, err
	}
	return s.complete(w)
}

// maxBackoff caps how many times its interval a failing watch waits (2^3).
const maxBackoff = 3

// NextCheck is when the watch is due: right away if never checked, else its interval
// (watch.interval if it has none) plus up to watch.jitter after the last check. After
// watch.backoff failures in a row the interval doubles with each failure, up to 8 times.
func (s *Service) NextCheck(w storage.Watch) time.Time {
	if w.LastChecked.IsZero() {
		return w.Added
	}
	interval := time.Duration(w.Interval)
	if interval <= 0 {
		interval = s.Options.Interval
	}
	if b := s.Options.Backoff; b > 0 && w.Failures >= b {
		interval <<= min(w.Failures-b+1, maxBackoff)
	}
	return w.LastChecked.Add(interval + jitter(w, s.Options.Jitter))
}

// jitter is the same for a watch until it's checked again.
func jitter(w storage.Watch, max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	h := fnv.New64a()
	fmt.Fprintf(h, "%d/%d", w.ID, w.LastChecked.Unix())
	return time.Duration(h.Sum64()%uint64(max/time.Second+1)) * time.Second
}

// GapsDue is true when the watch's gap filling is due (see watch.gap_interval).
func (s *Service) GapsDue(w storage.Watch, now time.Time) bool {
	return w.Gaps && s.Options.GapInterval > 0 && !now.Before(w.LastGapCheck.Add(s.Options.GapInterval))
}

// Due are the enabled watches due at now, by next check.
func (s *Service) Due(ctx context.Context, now time.Time) ([]Watch, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	var due []Watch
	for _, w := range all {
		if w.Enabled && !w.ProviderMissing && !w.NextCheck.After(now) {
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

// Runs lists the check runs (with what each watch got), latest first.
func (s *Service) Runs(ctx context.Context, filter RunFilter) ([]CheckRun, error) {
	return s.Watches.Runs(filter)
}

// RunDetails is the check run with the id, ErrNotFound if none.
func (s *Service) RunDetails(ctx context.Context, id int64) (CheckRun, error) {
	run, found, err := s.Watches.Run(id)
	if err != nil {
		return CheckRun{}, err
	}
	if !found {
		return CheckRun{}, fmt.Errorf("check run %d: %w", id, ErrNotFound)
	}
	return run, nil
}

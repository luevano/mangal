// Package watch keeps watched mangas up to date: each watch is one provider's manga,
// checked for chapters newer than the last one the library has (and optionally the gaps
// below it), downloaded with client.Download and recorded as check runs.
//
// Service is what the CLI, `mangal serve` and the TUI use; it never prints.
package watch

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/selector"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/luevano/mangal/v5/util/cron"
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
	// ErrBusy is a check of providers another check or a download has (in this or another
	// process).
	ErrBusy = errors.New("providers busy with another check or a download")
	// ErrNotFound is a watch id that doesn't exist.
	ErrNotFound = storage.ErrWatchNotFound
	// ErrExists is watching a manga that's already watched.
	ErrExists = storage.ErrWatchExists
	// ErrMangaNotFound is a watch whose manga the provider doesn't have (any more): its
	// manga id is wrong, it needs a re-link.
	ErrMangaNotFound = errors.New("manga not found")
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
	// NextScheduled is when the scheduler checks it: NextCheck, or when that's past, the
	// first tick after mangal serve started (after now without a server), missed ticks
	// aren't caught up.
	NextScheduled time.Time `json:"next_scheduled"`
	// NextGapCheck is when its gap filling is due, see Service.NextGapCheck; zero when it
	// has none or watch.gap_schedule is off.
	NextGapCheck time.Time `json:"next_gap_check,omitzero"`
	// ProviderMissing is a provider that isn't installed: the checks skip it (without an
	// error) until it is again.
	ProviderMissing bool `json:"provider_missing,omitempty"`
	// Bindings are the manga's metadata bindings (source, id, default), the default first.
	Bindings []storage.Binding `json:"bindings,omitempty"`
	// LibraryIssue is why it isn't linked to its library manga, with what to do; empty
	// when it is, or when nothing was downloaded yet (see libraryIssue). The scheduler
	// skips such a watch: a check would download into a folder of its own.
	LibraryIssue string `json:"library_issue,omitempty"`
	// LibraryElsewhere is the issue of a library manga in another provider's directory:
	// even an explicit check is refused until it's moved.
	LibraryElsewhere bool `json:"library_elsewhere,omitempty"`
	// LibraryNote is why a watch without an issue has no library manga (nothing matches
	// it yet), empty when it has one.
	LibraryNote string `json:"library_note,omitempty"`
	// Status is the publication status of its library manga (storage.Manga.Status), empty
	// when unknown.
	Status string `json:"status,omitempty"`
	// Hold is its hold mark (storage.HoldHiatus, HoldEnded): its library manga's, else its
	// own; empty when it isn't on hold. A watch on hold is paused.
	Hold string `json:"hold,omitempty"`
}

// Scheduled is true when the scheduler checks it: enabled, not on hold, its provider
// installed, no re-link needed, no library issue.
func (w Watch) Scheduled() bool {
	return w.Enabled && w.Hold == "" && !w.ProviderMissing && !w.NeedsRelink && w.LibraryIssue == ""
}

// LibraryProblem is why it has no library manga: its issue, else its note; empty when
// it's linked.
func (w Watch) LibraryProblem() string {
	if w.LibraryIssue != "" {
		return w.LibraryIssue
	}
	return w.LibraryNote
}

// LibraryHold is the short status of a watch the scheduler skips for its library issue,
// empty without one.
func (w Watch) LibraryHold() string {
	switch {
	case w.LibraryElsewhere:
		return "not in library: move it"
	case w.LibraryIssue != "":
		return "not in library: library scan"
	}
	return ""
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
	// Schedule is when the watches without their own are due (watch.schedule), the
	// zero Schedule is DefaultSchedule.
	Schedule cron.Schedule
	// Jitter is up to how long after a tick a provider's watches are due.
	Jitter time.Duration
	// GapSchedule is when gap filling is due on the watches that have it, zero never.
	GapSchedule cron.Schedule
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
	// CoverageMin is the share (0-1) of the library's chapters a provider must have not to
	// be suggested another (Coverage); CoverageExempt are the providers never suggested one.
	CoverageMin    float64
	CoverageExempt []string
}

// DefaultSchedule is watch.schedule's default: every 4 hours on the hour.
var DefaultSchedule = cron.MustParse(config.Watch.Schedule.Default)

// ParseSchedule is a watch's schedule: empty for the default (the zero Schedule), else a
// cron schedule (util/cron).
func ParseSchedule(spec string) (cron.Schedule, error) {
	if strings.TrimSpace(spec) == "" {
		return cron.Schedule{}, nil
	}
	return cron.Parse(spec)
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
	schedule, _ := ParseSchedule(config.Watch.Schedule.Get())
	gapSchedule, _ := ParseSchedule(config.Watch.GapSchedule.Get())
	return Options{
		Schedule:          schedule,
		Jitter:            duration(config.Watch.Jitter.Get(), 0),
		GapSchedule:       gapSchedule,
		Gaps:              config.Watch.Gaps.Get(),
		ParallelProviders: config.Watch.ParallelProviders.Get(),
		Backoff:           config.Watch.Backoff.Get(),
		Notify:            config.Watch.Notify.Get(),
		ProviderDirs:      config.Download.Provider.CreateDir.Get(),
		LockStale:         10 * time.Minute,
		CoverageMin:       float64(config.Watch.CoverageMin.Get()) / 100,
		CoverageExempt:    config.Watch.CoverageExempt.Get(),
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
	// Providers are the installed providers' infos (to tell their directories); nil = none.
	Providers func() ([]libmangal.ProviderInfo, error)
	// Notifier may be nil.
	Notifier Notifier
	// Now is time.Now, swapped in tests.
	Now func() time.Time
	// OnError is told about errors that don't fail anything (a notification that couldn't
	// be sent), may be nil.
	OnError func(error)
	// Events broadcasts the progress of the checks (Subscribe), may be nil.
	Events *Hub
	// Operation runs do as an operation "mangal library undo" can undo (client.Operation:
	// a snapshot of the data db first) and returns the snapshot; nil runs it as is.
	Operation func(kind, summary string, do func() error) (snapshot string, err error)
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
		Providers: providerInfos,
		Notifier:  notifier,
		Now:       time.Now,
		Events:    NewHub(),
		Operation: func(kind, summary string, do func() error) (string, error) {
			return client.Operation(kind, summary, func(library.Journal) error { return do() })
		},
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
	// Schedule overrides watch.schedule when not empty (a cron schedule).
	Schedule string
}

// Add watches a manga. A manga already watched is ErrExists.
func (s *Service) Add(ctx context.Context, opts AddOptions) (Watch, error) {
	if _, err := ParseSchedule(opts.Schedule); err != nil {
		return Watch{}, err
	}
	if err := checkFrom(opts.From); err != nil {
		return Watch{}, err
	}
	opts.Schedule = strings.TrimSpace(opts.Schedule)
	if opts.Offline {
		return s.addOffline(opts)
	}
	src, err := s.Source(ctx, opts.Provider)
	if err != nil {
		return Watch{}, err
	}
	defer CloseSource(src)
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
		Schedule: opts.Schedule,
		Gaps:     s.Options.Gaps,
		From:     opts.From,
		Added:    s.now(),
	}
	if opts.Gaps != nil {
		w.Gaps = *opts.Gaps
	}
	ids := storage.MangaMetadataIDs(manga)
	if opts.AnilistID != 0 {
		ids = append(ids, metadata.ID{Raw: strconv.Itoa(opts.AnilistID), Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist})
	}
	if match, err := s.Library.Match(info, w.MangaID, ids, s.Options.ProviderDirs); err != nil {
		return Watch{}, err
	} else if match.Linked {
		w.LibraryManga = match.Manga.ID
	}
	if w, err = s.Watches.Add(w); err != nil {
		return Watch{}, err
	}
	if err := s.linkSource(w); err != nil {
		return Watch{}, err
	}
	if err := s.bindAnilist(w, opts.AnilistID); err != nil {
		return Watch{}, err
	}
	return s.complete(w)
}

// linkSource makes the watched manga a source of its library manga (e.g. a folder adopted
// by a scan, linked by a metadata id), unless another library manga has it or the
// library manga has one of the provider already.
func (s *Service) linkSource(w storage.Watch) error {
	if w.LibraryManga == 0 {
		return nil
	}
	_, err := s.Library.AddSource(w.LibraryManga, storage.Source{Provider: w.Provider, MangaID: w.MangaID, Title: w.Title})
	return err
}

// link links the watch without a library manga to the one matching it (by its bindings
// and ids), with the watched manga as its source.
func (s *Service) link(w storage.Watch, ids []metadata.ID) (storage.Watch, error) {
	if w.LibraryManga != 0 {
		return w, nil
	}
	match, err := s.matchLibrary(s.providerInfo()(w.Provider), w, ids)
	if err != nil || !match.Linked {
		return w, err
	}
	if err := s.Watches.SetLibraryManga(w.ID, match.Manga.ID); err != nil {
		return w, err
	}
	w.LibraryManga = match.Manga.ID
	return w, s.linkSource(w)
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
		Schedule: opts.Schedule,
		Gaps:     s.Options.Gaps,
		From:     opts.From,
		Added:    s.now(),
	}
	if strings.HasPrefix(id, storage.PlaceholderPrefix) {
		w.NeedsRelink, w.RelinkReason = true, "placeholder manga id"
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
	if w, err = s.link(w, nil); err != nil {
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

// providerInfos are the installed providers' infos, for Service.Providers.
func providerInfos() ([]libmangal.ProviderInfo, error) {
	loaders, err := manager.Loaders()
	if err != nil {
		return nil, err
	}
	infos := make([]libmangal.ProviderInfo, len(loaders))
	for i, l := range loaders {
		infos[i] = l.Info()
	}
	return infos, nil
}

// providerInfo looks up a provider's info by id (an old id too), just its id as its name
// when it isn't installed.
func (s *Service) providerInfo() func(string) libmangal.ProviderInfo {
	var infos []libmangal.ProviderInfo
	if s.Providers != nil {
		var err error
		if infos, err = s.Providers(); err != nil {
			s.warn(fmt.Errorf("installed providers: %w", err))
		}
	}
	return func(id string) libmangal.ProviderInfo {
		for _, info := range infos {
			if info.Matches(id) {
				return info
			}
		}
		return libmangal.ProviderInfo{ID: id, Name: id}
	}
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
	return selector.Find(ctx, src, opts.MangaID, opts.Query, opts.Selector)
}

// List lists every watch, by id.
func (s *Service) List(ctx context.Context) ([]Watch, error) {
	list, err := s.Watches.List()
	if err != nil {
		return nil, err
	}
	installed := s.installed()
	since := s.scheduledSince()
	view, err := s.libraryView()
	if err != nil {
		return nil, err
	}
	watches := make([]Watch, len(list))
	for i, w := range list {
		if watches[i], err = s.completeWith(w, since, view); err != nil {
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
	id := strconv.Itoa(anilistID)
	set := func() error {
		return s.bindings().Set(storage.Binding{Provider: w.Provider, MangaID: w.MangaID, Source: metadata.IDCodeAnilist,
			ID: id, Updated: s.now()})
	}
	previous, err := s.anilistBinding(w)
	if err != nil {
		return err
	}
	if previous != "" && previous != id {
		return s.lossy(fmt.Sprintf("bind %s %s to AniList %s (was %s)", w.Provider, w.MangaID, id, previous), set)
	}
	return set()
}

// unbindAnilist removes the watched manga's AniList binding.
func (s *Service) unbindAnilist(w storage.Watch) error {
	previous, err := s.anilistBinding(w)
	if err != nil || previous == "" {
		return err
	}
	return s.lossy(fmt.Sprintf("unbind %s %s from AniList %s", w.Provider, w.MangaID, previous), func() error {
		_, err := s.bindings().Remove(w.Provider, w.MangaID, metadata.IDCodeAnilist)
		return err
	})
}

// anilistBinding is the AniList id the watched manga is bound to, "" for none.
func (s *Service) anilistBinding(w storage.Watch) (string, error) {
	list, err := s.bindings().Get(w.Provider, w.MangaID)
	if err != nil {
		return "", err
	}
	for _, b := range list {
		if b.Source == metadata.IDCodeAnilist {
			return b.ID, nil
		}
	}
	return "", nil
}

// lossy runs a change that loses data (a binding) as an undoable operation, when the
// service has them.
func (s *Service) lossy(summary string, do func() error) error {
	if s.Operation == nil {
		return do()
	}
	_, err := s.Operation("bind", summary, do)
	return err
}

func (s *Service) complete(w storage.Watch) (Watch, error) {
	view, err := s.libraryView()
	if err != nil {
		return Watch{}, err
	}
	return s.completeWith(w, s.scheduledSince(), view)
}

// libraryView is the library as the watches' links are worked out from, loaded once for
// a list of watches.
type libraryView struct {
	mangas    []storage.Manga
	downloads map[int64]bool
	provider  func(string) libmangal.ProviderInfo
}

func (s *Service) libraryView() (libraryView, error) {
	mangas, err := s.Library.Mangas()
	if err != nil {
		return libraryView{}, err
	}
	downloads, err := s.Watches.WithDownloads()
	if err != nil {
		return libraryView{}, err
	}
	return libraryView{mangas: mangas, downloads: downloads, provider: s.providerInfo()}, nil
}

// completeWith is complete for a scheduler started at since (see scheduledSince).
func (s *Service) completeWith(w storage.Watch, since time.Time, view libraryView) (Watch, error) {
	full := Watch{Watch: w, NextCheck: s.NextCheck(w), NextScheduled: s.NextCheckSince(w, since), NextGapCheck: s.NextGapCheck(w)}
	bindings, err := s.bindings().Get(w.Provider, w.MangaID)
	if err != nil {
		return Watch{}, err
	}
	libraryManga := w.LibraryManga
	if libraryManga == 0 {
		ids := storage.BindingIDs(bindings)
		match := storage.MatchLibrary(view.mangas, view.provider(w.Provider), w.MangaID, ids, s.Options.ProviderDirs)
		switch {
		case match.Linked:
			// only shown: reading writes nothing (an undo would see it as a change), the
			// next check links it
			libraryManga = match.Manga.ID
			full.LibraryManga = libraryManga
		default:
			full.LibraryIssue = libraryIssue(match, view.downloads[w.ID])
			full.LibraryElsewhere = match.Elsewhere
			if full.LibraryIssue == "" {
				full.LibraryNote = libraryNote(ids)
			}
		}
	}
	full.fillBindings(bindings)
	full.Hold = w.Hold
	for _, m := range view.mangas {
		if m.ID == libraryManga {
			full.Status, full.Hold = m.Status, cmp.Or(m.Hold, w.Hold)
		}
	}
	last, err := s.lastDownloaded(w, libraryManga)
	if err != nil {
		return Watch{}, err
	}
	full.LastDownloaded = last
	return full, nil
}

// libraryIssue is why a watch without a library manga isn't linked: its manga is in the
// library where its downloads don't go (another provider's directory), or chapters it
// downloaded aren't in the library. Nothing when it matches (it's linked on its next
// check) or never downloaded anything.
func libraryIssue(match storage.LibraryMatch, downloaded bool) string {
	switch {
	case match.Linked:
		return ""
	case match.Elsewhere:
		return fmt.Sprintf("in the library under %s (%s): move it there (X in the library)", filepath.Base(match.Manga.Parent), match.Manga.Title)
	case downloaded:
		return "its downloads aren't in the library (library scan?)"
	}
	return ""
}

// libraryNote is why a watch has no library manga when nothing matches it.
func libraryNote(ids []metadata.ID) string {
	if len(ids) == 0 {
		return "no library manga: no metadata ids to match by (bind its AniList id)"
	}
	var parts []string
	for _, id := range ids {
		parts = append(parts, string(id.Code)+" "+id.Raw)
	}
	return "no library manga with " + strings.Join(parts, ", ") + " where its downloads go"
}

// ElsewhereReason is why a watch whose library manga is in another provider's directory
// isn't checked, with what moves it.
func ElsewhereReason(m storage.Manga, provider string) string {
	return fmt.Sprintf("not checked: its library manga %q is under %s, move it first (X in the library, or mangal library move %d %s)",
		m.Title, filepath.Base(m.Parent), m.ID, provider)
}

// matchLibrary is the library manga of the watch (its source, else a folder sharing a
// metadata id of the manga or of the watch's bindings), see storage.Library.Match.
func (s *Service) matchLibrary(provider libmangal.ProviderInfo, w storage.Watch, ids []metadata.ID) (storage.LibraryMatch, error) {
	bindings, err := s.bindings().Get(w.Provider, w.MangaID)
	if err != nil {
		return storage.LibraryMatch{}, err
	}
	return s.Library.Match(provider, w.MangaID, append(slices.Clone(ids), storage.BindingIDs(bindings)...), s.Options.ProviderDirs)
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

// Remove stops watching, the history of its checks stays. "mangal library undo" undoes it.
func (s *Service) Remove(ctx context.Context, id int64) error {
	_, err := s.RemoveWatches(ctx, []int64{id})
	return err
}

// RemoveWatches stops watching the watches with the ids (ErrWatchNotFound when one isn't),
// as one operation "mangal library undo" can undo; it returns its snapshot.
func (s *Service) RemoveWatches(ctx context.Context, ids []int64) (string, error) {
	var titles []string
	for _, id := range ids {
		w, err := s.Watches.Get(id)
		if err != nil {
			return "", err
		}
		titles = append(titles, fmt.Sprintf("%d %q", w.ID, w.Title))
	}
	do := func() error {
		for _, id := range ids {
			if err := s.Watches.Remove(id); err != nil {
				return err
			}
		}
		return nil
	}
	if s.Operation == nil || len(ids) == 0 {
		return "", do()
	}
	summary := "stop watching " + strings.Join(titles, ", ")
	if len(ids) > 3 {
		summary = fmt.Sprintf("stop watching %d mangas", len(ids))
	}
	return s.Operation("unwatch", summary, do)
}

// SetEnabled pauses (false) or resumes the watch. A watch on hold isn't resumed
// (ErrHeld): clearing its mark does it (Hold).
func (s *Service) SetEnabled(ctx context.Context, id int64, enabled bool) (Watch, error) {
	if enabled {
		w, err := s.Get(ctx, id)
		if err != nil {
			return Watch{}, err
		}
		if w.Hold != "" {
			return Watch{}, fmt.Errorf("watch %d is %w (%s): clearing the mark resumes it (mangal watch hold %d --clear)", id, ErrHeld, w.Hold, id)
		}
	}
	return s.update(id, func(w *storage.Watch) {
		if enabled && !w.Enabled {
			w.Resumed = s.now()
		}
		w.Enabled = enabled
	})
}

// UpdateOptions are the settings of a watch to change, nil keeps them.
type UpdateOptions struct {
	// Schedule overrides watch.schedule (a cron schedule), empty goes back to it.
	Schedule *string
	Gaps     *bool
	// AnilistID binds an AniList manga (the default binding), 0 unbinds it.
	AnilistID *int
	// From treats the chapters up to it as done.
	From *float32
	// Skip replaces the chapter numbers never to download (empty clears them).
	Skip *[]float32
}

// Update changes the watch's settings.
func (s *Service) Update(ctx context.Context, id int64, opts UpdateOptions) (Watch, error) {
	if err := checkFrom(opts.From); err != nil {
		return Watch{}, err
	}
	if opts.Schedule != nil {
		if _, err := ParseSchedule(*opts.Schedule); err != nil {
			return Watch{}, err
		}
	}
	if opts.AnilistID != nil {
		w, err := s.Watches.Get(id)
		if err != nil {
			return Watch{}, err
		}
		if *opts.AnilistID == 0 {
			if err := s.unbindAnilist(w); err != nil {
				return Watch{}, err
			}
		} else if err := s.bindAnilist(w, *opts.AnilistID); err != nil {
			return Watch{}, err
		}
	}
	return s.update(id, func(w *storage.Watch) {
		if opts.Schedule != nil {
			w.Schedule = strings.TrimSpace(*opts.Schedule)
		}
		if opts.Gaps != nil {
			w.Gaps = *opts.Gaps
		}
		if opts.From != nil {
			w.From = opts.From
		}
		if opts.Skip != nil {
			w.Skip = AddNumbers(nil, *opts.Skip)
		}
	})
}

// RelinkOptions say which manga a watch moves to: the one with MangaID, or the one
// Selector picks from a search of Query (a "mid:<id>" query is the id), as AddOptions.
type RelinkOptions struct {
	// Provider is the manga's provider, the watch's if empty.
	Provider string `json:"provider,omitempty"`
	MangaID  string `json:"manga_id,omitempty"`
	Query    string `json:"query,omitempty"`
	// Selector is a manga selector (client/selector), exact if empty.
	Selector string `json:"selector,omitempty"`
	// AnilistID binds an AniList manga too (the default binding), 0 keeps the bindings.
	AnilistID int `json:"anilist_id,omitempty"`
}

// Relink points the watch to another manga (e.g. the site changed its ids, or the
// provider moved), keeping the watch: its id, runs, schedule, settings and metadata
// bindings (moved to the new manga). It clears the re-link flag and the failures. The
// library manga is worked out again (Library.MangaFor), kept when none matches. A manga
// another watch has is ErrExists.
func (s *Service) Relink(ctx context.Context, id int64, opts RelinkOptions) (Watch, error) {
	w, err := s.Watches.Get(id)
	if err != nil {
		return Watch{}, err
	}
	provider := cmp.Or(opts.Provider, w.Provider)
	src, err := s.Source(ctx, provider)
	if err != nil {
		return Watch{}, err
	}
	defer CloseSource(src)
	manga, err := s.find(ctx, src, AddOptions{MangaID: opts.MangaID, Query: opts.Query, Selector: opts.Selector})
	if err != nil {
		return Watch{}, err
	}
	info := src.Info()
	if other, found, err := s.Watches.Find(info.ID, manga.Info().ID); err != nil {
		return Watch{}, err
	} else if found && other.ID != id {
		return Watch{}, fmt.Errorf("re-link watch %d to %q from %s: watch %d has it: %w", id, manga.Info().Title, info.ID, other.ID, ErrExists)
	}
	if opts.AnilistID != 0 {
		if _, err := src.BoundMetadata(ctx, metadata.IDCodeAnilist, strconv.Itoa(opts.AnilistID)); err != nil {
			return Watch{}, err
		}
	}
	// by the manga's ids, and the bindings of the watch (they move with it) and of the new manga
	ids := storage.MangaMetadataIDs(manga)
	if opts.AnilistID != 0 {
		ids = append(ids, metadata.ID{Raw: strconv.Itoa(opts.AnilistID), Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist})
	}
	for _, key := range [][2]string{{w.Provider, w.MangaID}, {info.ID, manga.Info().ID}} {
		bindings, err := s.bindings().Get(key[0], key[1])
		if err != nil {
			return Watch{}, err
		}
		ids = append(ids, storage.BindingIDs(bindings)...)
	}
	var libraryManga int64
	if match, err := s.Library.Match(info, manga.Info().ID, ids, s.Options.ProviderDirs); err != nil {
		return Watch{}, err
	} else if match.Linked {
		libraryManga = match.Manga.ID
	}
	title := cmp.Or(manga.Info().Title, w.Title)
	if err := s.Watches.Relink(id, info.ID, manga.Info().ID, title, libraryManga, s.now()); err != nil {
		return Watch{}, err
	}
	if w, err = s.Watches.Get(id); err != nil {
		return Watch{}, err
	}
	if libraryManga != 0 {
		if err := s.linkSource(w); err != nil {
			return Watch{}, err
		}
	}
	if err := s.bindAnilist(w, opts.AnilistID); err != nil {
		return Watch{}, err
	}
	return s.Get(ctx, id)
}

func (s *Service) update(id int64, change func(*storage.Watch)) (Watch, error) {
	w, err := s.Watches.Update(id, change)
	if err != nil {
		return Watch{}, err
	}
	return s.complete(w)
}

// maxBackoff caps how many ticks a failing watch skips (2^3 = it waits 8 ticks).
const maxBackoff = 3

// Schedule is the watch's schedule: its own, else watch.schedule.
func (s *Service) Schedule(w storage.Watch) cron.Schedule {
	if w.Schedule != "" {
		sched, err := cron.Parse(w.Schedule)
		if err == nil {
			return sched
		}
		s.warn(fmt.Errorf("watch %d: %w, using watch.schedule", w.ID, err))
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

// Runs lists the check runs (with what each watch got), latest first.
func (s *Service) Runs(ctx context.Context, filter RunFilter) ([]CheckRun, error) {
	return s.Watches.Runs(filter)
}

// RemovedRun is a watch run removed from the history.
type RemovedRun struct {
	// Run is the removed one, without its chapters.
	Run WatchRun `json:"run"`
	// CheckRunRemoved is true when its check run had nothing else and went too.
	CheckRunRemoved bool `json:"check_run_removed,omitempty"`
	// Snapshot is the data db's snapshot taken before ("mangal library undo" restores it).
	Snapshot string `json:"snapshot,omitempty"`
}

// RemoveRun removes a watch run (the check of one watch in a check run) and its chapters
// from the history, as an operation "mangal library undo" can undo. A check run left
// without watch runs goes too. Unknown = storage.ErrRunNotFound.
func (s *Service) RemoveRun(ctx context.Context, id int64) (RemovedRun, error) {
	removed, err := s.RemoveRuns(ctx, []int64{id})
	if len(removed) == 0 {
		return RemovedRun{}, err
	}
	return removed[0], err
}

// RemoveRuns removes the watch runs as RemoveRun does, as one operation (every one's
// Snapshot is its snapshot). Nothing is removed when one is unknown or unfinished.
func (s *Service) RemoveRuns(ctx context.Context, ids []int64) ([]RemovedRun, error) {
	// no snapshot for nothing
	crashed := map[int64]bool{}
	for _, id := range ids {
		if _, finished, err := s.Watches.WatchRun(id); err != nil {
			return nil, fmt.Errorf("remove %w", err)
		} else if !finished {
			// a check that crashed never finishes: running, it holds provider locks
			checking, err := s.checking()
			if err != nil {
				return nil, err
			}
			if checking {
				return nil, fmt.Errorf("remove watch run %d: %w", id, storage.ErrRunUnfinished)
			}
			crashed[id] = true
		}
	}
	var removed []RemovedRun
	do := func() error {
		for _, id := range ids {
			var r RemovedRun
			var err error
			remove := s.Watches.RemoveWatchRun
			if crashed[id] {
				remove = s.Watches.RemoveCrashedWatchRun
			}
			if r.Run, r.CheckRunRemoved, err = remove(id); err != nil {
				return err
			}
			removed = append(removed, r)
		}
		return nil
	}
	if s.Operation == nil || len(ids) == 0 {
		return removed, do()
	}
	strs := make([]string, len(ids))
	for i, id := range ids {
		strs[i] = strconv.FormatInt(id, 10)
	}
	summary := fmt.Sprintf("remove watch run %s from the history", strs[0])
	if len(ids) > 1 {
		summary = fmt.Sprintf("remove watch runs %s from the history", strings.Join(strs, ", "))
	}
	snapshot, err := s.Operation("history", summary, do)
	for i := range removed {
		removed[i].Snapshot = snapshot
	}
	return removed, err
}

// checking is true when a watch check runs, here or in another process (it holds
// provider locks).
func (s *Service) checking() (bool, error) {
	holders, err := s.Watches.Holders(lockPrefix, s.lockStale())
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(holders, func(h storage.LockHolder) bool { return h.Purpose == client.LockForWatch }), nil
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

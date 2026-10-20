package core

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/progress"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/health"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// JobKinds are the maintenance jobs by kind (the Jobs screen and page): watch checks,
// source checks (health L11), source addresses, the library scan and the network health
// checks, each with its state, settings and runs. mangal serve runs the scheduled ones.
type JobKinds interface {
	// List is every kind, without the per-provider details' counts.
	List(ctx context.Context) ([]JobKind, error)
	// Get is the kind with its per-provider details, a *NotFoundError for an unknown one.
	Get(ctx context.Context, kind jobs.Kind) (JobKind, error)
	// Run starts a job of the kind: every item (all), or only the missing ones (the due
	// watches, the sources not checked within their interval, those without an address).
	// ErrRefused while one runs, ErrNothingToDo when there's nothing to do.
	Run(ctx context.Context, kind jobs.Kind, all bool) (Job, error)
	// Pause stops (or resumes) scheduling the kind; a running job goes on.
	Pause(ctx context.Context, kind jobs.Kind, paused bool) (JobKind, error)
	// Set changes the kind's settings in the config file.
	Set(ctx context.Context, kind jobs.Kind, s JobSettings) (JobKind, error)
}

// JobKind is a kind of maintenance job as it is now.
type JobKind struct {
	Kind  jobs.Kind `json:"kind"`
	Title string    `json:"title"`
	About string    `json:"about"`
	// Enabled is false when mangal serve doesn't schedule it (its config key), Paused when
	// its scheduling is paused (Pause); Manual for a kind it never schedules.
	Enabled bool `json:"enabled"`
	Paused  bool `json:"paused"`
	Manual  bool `json:"manual"`
	// Schedule is its cron schedule (watch checks) or interval, ScheduleKey its config
	// key; empty for a manual kind.
	Schedule    string `json:"schedule,omitempty"`
	ScheduleKey string `json:"schedule_key,omitempty"`
	// ProvidersKey is its per-provider config key, empty for a kind without.
	ProvidersKey string `json:"providers_key,omitempty"`
	// Last is its last run (nil: none kept), Next when mangal serve runs it next (zero:
	// not scheduled, NoNext says why).
	Last   *JobRun   `json:"last,omitempty"`
	Next   time.Time `json:"next,omitzero"`
	NoNext string    `json:"no_next,omitempty"`
	// Running is its active job here, nil when none; Busy what runs it elsewhere (the
	// scheduler's watch checks, another mangal's).
	Running *Job   `json:"running,omitempty"`
	Busy    string `json:"busy,omitempty"`
	// Missing is how many items a "run missing" does now, nil for a kind where it's the
	// same as all.
	Missing *int `json:"missing,omitempty"`
	// MissingWhat names them ("due watches").
	MissingWhat string `json:"missing_what,omitempty"`
	// Providers are its per-provider settings (and, from Get, counts).
	Providers []JobProvider `json:"providers,omitempty"`
}

// JobRun is a kind's run as kept.
type JobRun struct {
	// Job is its job in the process that ran it (0: none), Run its watch check run or
	// mangal serve's health job (0: none).
	Job int64 `json:"job,omitempty"`
	Run int64 `json:"run,omitempty"`
	// All is a run of every item, not only the missing ones.
	All      bool      `json:"all"`
	Started  time.Time `json:"started,omitzero"`
	Finished time.Time `json:"finished,omitzero"`
	Summary  string    `json:"summary,omitempty"`
	Error    string    `json:"error,omitempty"`
}

// JobProvider is a provider's settings for a kind.
type JobProvider struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
	On       bool   `json:"on"`
	// Interval is its own interval (cron schedule for the watch checks), empty for the
	// kind's; Default the prebuilt default ("off" for MangaPlus' maintenance jobs).
	Interval string `json:"interval,omitempty"`
	Default  string `json:"default,omitempty"`
	// Items are its watches or sources (Get only), Missing those a "run missing" does,
	// Next when its next item is due.
	Items   int       `json:"items"`
	Missing int       `json:"missing"`
	Next    time.Time `json:"next,omitzero"`
}

// JobSettings change a kind's settings, nil fields stay.
type JobSettings struct {
	Enabled  *bool   `json:"enabled,omitempty"`
	Schedule *string `json:"schedule,omitempty"`
	// Providers are provider id = on, off or an interval; empty for the prebuilt default.
	Providers map[string]string `json:"providers,omitempty"`
	// DelayMin and DelayMax are the pause between requests (jobs.delay_min/max), shared
	// by the kinds.
	DelayMin *string `json:"delay_min,omitempty"`
	DelayMax *string `json:"delay_max,omitempty"`
}

// KindsOptions are a JobKinds' hooks.
type KindsOptions struct {
	// Origin is the origin of the jobs it starts ("api", a TUI window key).
	Origin string
	// Started is when the scheduler started (mangal serve's own), zero for the running
	// mangal serve's (its lock): a kind's next run counts from it, none without a serve.
	Started time.Time
	// OnHealth is told a health run's report (source check, network health) once it's
	// over, may be nil; what it answers is kept as the run's Run (serve's health job).
	OnHealth func(opts health.Options, started time.Time, report health.Report, err error) int64
	// Changed is told a kind's state or settings changed (serve's scheduler wakes), may
	// be nil.
	Changed func()
}

// The kinds' order on the Jobs screen.
var kindOrder = []jobs.Kind{jobs.KindCheck, jobs.KindSourceCheck, jobs.KindSourceURLs, jobs.KindScan, jobs.KindHealth}

// Kinds is the JobKinds over the manager m: the jobs started run there.
func (c *Core) Kinds(m *jobs.Manager, opts KindsOptions) JobKinds {
	return &localKinds{core: c, m: m, opts: opts}
}

type localKinds struct {
	core *Core
	m    *jobs.Manager
	opts KindsOptions
	// mu guards the read-modify-write of the kinds' kept state in this process
	mu sync.Mutex
}

// kindState is what's kept of a kind in the cache db.
type kindState struct {
	Paused bool    `json:"paused,omitempty"`
	Last   *JobRun `json:"last,omitempty"`
	// Checked are when each source was last looked up (provider id, NUL, manga id).
	Checked map[string]time.Time `json:"checked,omitempty"`
}

const (
	// kindStartDelay is how long after mangal serve starts an overdue run waits.
	kindStartDelay = 15 * time.Minute
	// kindMinGap is the shortest time between two scheduled runs of a kind.
	kindMinGap = 15 * time.Minute
)

func (k *localKinds) kv() storage.KV {
	return storage.KV{DB: k.core.env.Storage.Cache, Table: storage.TableOther, Owner: storage.OwnerMangal + "/jobs"}
}

func (k *localKinds) state(kind jobs.Kind) (kindState, error) {
	var st kindState
	_, err := k.kv().Get(kind.String(), &st)
	if err == nil && st.Last == nil && kind == jobs.KindSourceCheck {
		// mangal serve's source check before the kinds kept only when it ran
		var last time.Time
		old := storage.KV{DB: k.core.env.Storage.Cache, Table: storage.TableOther, Owner: storage.OwnerMangal + "/serve"}
		if ok, _ := old.Get("source-check", &last); ok && !last.IsZero() {
			st.Last = &JobRun{Started: last, Finished: last}
		}
	}
	return st, err
}

func (k *localKinds) update(kind jobs.Kind, f func(*kindState)) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	// shortcut: another process writing the same kind at once loses one write, a
	// kind runs in one process at a time (mangal serve's schedules)
	st, err := k.state(kind)
	if err != nil {
		return err
	}
	f(&st)
	return k.kv().Set(kind.String(), st)
}

func (k *localKinds) changed() {
	if k.opts.Changed != nil {
		k.opts.Changed()
	}
}

func unknownKind(kind jobs.Kind) error {
	return &NotFoundError{What: fmt.Sprintf("no job kind %q (%s)", kind, strings.Join(kindNames(), ", "))}
}

func kindNames() []string {
	names := make([]string, len(kindOrder))
	for i, k := range kindOrder {
		names[i] = k.String()
	}
	return names
}

// ParseJobKind is a kind of the Jobs screen by its name (check, sources, urls, scan,
// health).
func ParseJobKind(name string) (jobs.Kind, error) {
	var kind jobs.Kind
	if err := kind.UnmarshalText([]byte(name)); err != nil || !slices.Contains(kindOrder, kind) {
		return kind, unknownKind(jobs.Kind(-1))
	}
	return kind, nil
}

// since is when the scheduler started, zero without a running mangal serve.
func (k *localKinds) since() time.Time {
	if !k.opts.Started.IsZero() {
		return k.opts.Started
	}
	if h, ok, err := k.core.env.Storage.Watches().Holder(watch.ServeLock, watch.ServeLockStale); err == nil && ok {
		return h.Acquired
	}
	return time.Time{}
}

// active is the kind's active job here.
func (k *localKinds) active(kind jobs.Kind) *Job {
	if k.m == nil {
		return nil
	}
	for _, j := range k.m.Active() {
		if j.Kind == kind {
			job := JobOf(j)
			return &job
		}
	}
	return nil
}

func (k *localKinds) List(ctx context.Context) ([]JobKind, error) {
	out := make([]JobKind, 0, len(kindOrder))
	for _, kind := range kindOrder {
		jk, err := k.get(ctx, kind, false)
		if err != nil {
			return nil, err
		}
		out = append(out, jk)
	}
	return out, nil
}

func (k *localKinds) Get(ctx context.Context, kind jobs.Kind) (JobKind, error) {
	return k.get(ctx, kind, true)
}

// providerSettings are the kind's per-provider settings and config key.
func providerSettings(kind jobs.Kind) (config.ProviderSettings, string) {
	switch kind {
	case jobs.KindCheck:
		return config.Jobs.Check.Providers.Fresh(), config.Jobs.Check.Providers.Key
	case jobs.KindSourceCheck:
		return config.Jobs.Sources.Providers.Fresh(), config.Jobs.Sources.Providers.Key
	case jobs.KindSourceURLs:
		return config.Jobs.URLs.Providers.Fresh(), config.Jobs.URLs.Providers.Key
	case jobs.KindHealth:
		return config.Jobs.Health.Providers.Fresh(), config.Jobs.Health.Providers.Key
	}
	return nil, ""
}

func (k *localKinds) get(ctx context.Context, kind jobs.Kind, details bool) (JobKind, error) {
	jk := JobKind{Kind: kind, Enabled: true, Running: k.active(kind)}
	st, err := k.state(kind)
	if err != nil {
		return jk, err
	}
	jk.Paused, jk.Last = st.Paused, st.Last
	settings, key := providerSettings(kind)
	jk.ProvidersKey = key
	since := k.since()
	switch kind {
	case jobs.KindCheck:
		jk.Title, jk.About = "Watch checks", "checks the watched mangas for new chapters and downloads them"
		jk.Enabled = config.Jobs.Check.Enabled.Fresh()
		jk.Schedule, jk.ScheduleKey = config.Watch.Schedule.Fresh(), config.Watch.Schedule.Key
		if err := k.checkState(ctx, &jk, settings, since); err != nil {
			return jk, err
		}
	case jobs.KindSourceCheck, jobs.KindSourceURLs:
		if kind == jobs.KindSourceCheck {
			jk.Title, jk.About = "Source checks", "checks that each library manga's source is still on its provider (health L11)"
			jk.Enabled = config.Jobs.Sources.Enabled.Fresh()
			jk.Schedule, jk.ScheduleKey = config.Health.SourceCheckInterval.Fresh(), config.Health.SourceCheckInterval.Key
			jk.MissingWhat = "sources not checked within their interval"
		} else {
			jk.Title, jk.About = "Source addresses", "looks up the web pages of the library sources without one"
			jk.Enabled = config.Jobs.URLs.Enabled.Fresh()
			jk.Schedule, jk.ScheduleKey = config.Jobs.URLs.Interval.Fresh(), config.Jobs.URLs.Interval.Key
			jk.MissingWhat = "sources without an address, not asked within their interval"
		}
		if err := k.sourcesState(&jk, st, settings, since); err != nil {
			return jk, err
		}
	case jobs.KindScan:
		jk.Title, jk.About, jk.Manual = "Library scan", "reads the download folder: chapters added, moved or removed, new folders", true
		jk.NoNext = "manual"
	case jobs.KindHealth:
		jk.Title, jk.About, jk.Manual = "Network health", "the health checks with the ones that ask the providers and AniList", true
		jk.NoNext = "manual"
		if err := k.healthProviders(&jk, settings); err != nil {
			return jk, err
		}
	default:
		return jk, unknownKind(kind)
	}
	if !details {
		for i := range jk.Providers {
			jk.Providers[i].Items, jk.Providers[i].Missing, jk.Providers[i].Next = 0, 0, time.Time{}
		}
	}
	return jk, nil
}

// noNext is why a scheduled kind has no next run, empty when it has one.
func noNext(jk JobKind, since time.Time) string {
	switch {
	case !jk.Enabled:
		return "turned off"
	case jk.Paused:
		return "paused"
	case since.IsZero():
		return "mangal serve isn't running (it runs the schedules)"
	}
	return ""
}

// provider is the kind's JobProvider of the provider.
func provider(loaders []libmangal.ProviderLoader, settings, defaults config.ProviderSettings, id string) JobProvider {
	p := JobProvider{Provider: id, Name: id, On: settings.On(id), Interval: settings.Override(id), Default: defaults[id]}
	if l, _, ok := libmangal.FindLoader(loaders, id); ok {
		p.Name = l.Info().Name
	}
	return p
}

// providersOf are the installed providers and those the settings name, plus extra, by
// name.
func providersOf(loaders []libmangal.ProviderLoader, settings, defaults config.ProviderSettings, extra ...string) []JobProvider {
	ids := slices.Collect(maps.Keys(settings))
	for _, l := range loaders {
		ids = append(ids, l.Info().ID)
	}
	ids = append(ids, extra...)
	slices.Sort(ids)
	var out []JobProvider
	for _, id := range slices.Compact(ids) {
		out = append(out, provider(loaders, settings, defaults, id))
	}
	slices.SortStableFunc(out, func(a, b JobProvider) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return out
}

func (k *localKinds) healthProviders(jk *JobKind, settings config.ProviderSettings) error {
	loaders, err := k.core.env.Loaders()
	if err != nil {
		return err
	}
	jk.Providers = providersOf(loaders, settings, kindDefaults(jk.Kind))
	return nil
}

// kindDefaults are the kind's prebuilt per-provider settings.
func kindDefaults(kind jobs.Kind) config.ProviderSettings {
	switch kind {
	case jobs.KindSourceCheck, jobs.KindSourceURLs, jobs.KindHealth:
		return config.MaintenanceDefaults()
	}
	return config.ProviderSettings{}
}

// checkState fills in the watch checks' runs, next run and due watches.
func (k *localKinds) checkState(ctx context.Context, jk *JobKind, settings config.ProviderSettings, since time.Time) error {
	m, err := k.core.env.Watches(ctx)
	if err != nil {
		return err
	}
	all, err := m.List(ctx)
	if err != nil {
		return err
	}
	if runs, err := m.Runs(ctx, watch.RunFilter{Limit: 1}); err == nil && len(runs) > 0 {
		r := runs[0]
		jk.Last = &JobRun{Run: r.ID, Started: r.Started, Finished: r.Finished, All: r.Kind != watch.KindScheduled,
			Summary: fmt.Sprintf("%s: %s, %s, %s", r.Kind, stringutil.Quantify(r.Watches, "watch", "watches"),
				stringutil.Quantify(r.NewChapters, "new chapter", "new chapters"), stringutil.Quantify(r.Errors, "error", "errors"))}
	}
	if status, err := m.Status(ctx); err == nil && status.Checking {
		var providers []string
		for _, p := range status.Providers {
			providers = append(providers, p.Provider)
		}
		jk.Busy = "checking " + strings.Join(providers, ", ")
	}
	loaders, err := k.core.env.Loaders()
	if err != nil {
		return err
	}
	now := k.core.env.Now()
	byProvider := map[string]*JobProvider{}
	var extra []string
	for _, w := range all {
		extra = append(extra, w.Provider)
	}
	jk.Providers = providersOf(loaders, settings, config.ProviderSettings{}, extra...)
	for i := range jk.Providers {
		byProvider[jk.Providers[i].Provider] = &jk.Providers[i]
	}
	missing := 0
	jk.NoNext = noNext(*jk, since)
	for _, w := range all {
		p := byProvider[w.Provider]
		if !w.Scheduled() {
			continue
		}
		p.Items++
		if !p.On {
			continue
		}
		if !w.NextCheck.After(now) {
			p.Missing++
			missing++
		}
		if p.Next.IsZero() || w.NextScheduled.Before(p.Next) {
			p.Next = w.NextScheduled
		}
		if jk.NoNext == "" && (jk.Next.IsZero() || w.NextScheduled.Before(jk.Next)) {
			jk.Next = w.NextScheduled
		}
	}
	jk.Missing, jk.MissingWhat = &missing, "due watches"
	if jk.NoNext == "" && jk.Next.IsZero() {
		jk.NoNext = "no watch to check"
	}
	return nil
}

// source is a library source a sources job looks at.
type source struct {
	manga             int64
	title             string
	provider, mangaID string
}

func sourceKey(provider, mangaID string) string { return provider + "\x00" + mangaID }

// kindInterval is the kind's interval, 0 when it has none (off).
func kindInterval(jk JobKind) time.Duration {
	d, err := time.ParseDuration(jk.Schedule)
	if err != nil || d < 0 {
		return 0
	}
	return d
}

// sources are the sources the kind looks at (all: every one), of providers installed and
// on, by provider id (the current one).
func (k *localKinds) sources(kind jobs.Kind, all bool, settings config.ProviderSettings) ([]source, []libmangal.ProviderLoader, error) {
	mangas, err := k.core.env.Storage.Library().Mangas()
	if err != nil {
		return nil, nil, err
	}
	loaders, err := k.core.env.Loaders()
	if err != nil {
		return nil, nil, err
	}
	var out []source
	add := func(m storage.Manga, s storage.Source) {
		if s.MangaID == "" || strings.HasPrefix(s.MangaID, storage.PlaceholderPrefix) {
			return
		}
		l, _, ok := libmangal.FindLoader(loaders, s.Provider)
		if !ok || !settings.On(l.Info().ID) {
			return
		}
		out = append(out, source{manga: m.ID, title: cmp.Or(s.Title, m.Title), provider: l.Info().ID, mangaID: s.MangaID})
	}
	for _, m := range mangas {
		if kind == jobs.KindSourceCheck {
			if len(m.Sources) > 0 {
				add(m, m.Sources[0])
			}
			continue
		}
		for _, s := range m.Sources {
			if all || !WebURL(s.URL) && s.Gone.IsZero() {
				add(m, s)
			}
		}
	}
	return out, loaders, nil
}

// due is when the source is due: an interval after it was last looked up, never looked
// up ones an interval after base.
func due(st kindState, s source, settings config.ProviderSettings, interval time.Duration, base time.Time) time.Time {
	if d, err := time.ParseDuration(settings.Override(s.provider)); err == nil && d > 0 {
		interval = d
	}
	from, ok := st.Checked[sourceKey(s.provider, s.mangaID)]
	if !ok {
		from = base
	}
	return from.Add(interval)
}

// sourcesState fills in a sources kind's next run and missing ones, by provider.
func (k *localKinds) sourcesState(jk *JobKind, st kindState, settings config.ProviderSettings, since time.Time) error {
	list, loaders, err := k.sources(jk.Kind, false, settings)
	if err != nil {
		return err
	}
	var extra []string
	for _, s := range list {
		extra = append(extra, s.provider)
	}
	jk.Providers = providersOf(loaders, settings, kindDefaults(jk.Kind), extra...)
	byProvider := map[string]*JobProvider{}
	for i := range jk.Providers {
		byProvider[jk.Providers[i].Provider] = &jk.Providers[i]
	}
	interval := kindInterval(*jk)
	now := k.core.env.Now()
	missing := 0
	jk.NoNext = noNext(*jk, since)
	if jk.NoNext == "" && interval == 0 {
		jk.NoNext = "turned off (" + jk.ScheduleKey + " is 0)"
	}
	// never looked up ones are due an interval after the last run (or the start)
	base := since
	if jk.Last != nil && jk.Last.Finished.After(base) {
		base = jk.Last.Finished
	}
	for _, s := range list {
		p := byProvider[s.provider]
		p.Items++
		if interval == 0 {
			continue
		}
		if !due(st, s, settings, interval, time.Time{}).After(now) {
			p.Missing++
			missing++
		}
		next := due(st, s, settings, interval, base)
		if p.Next.IsZero() || next.Before(p.Next) {
			p.Next = next
		}
		if jk.NoNext == "" && (jk.Next.IsZero() || next.Before(jk.Next)) {
			jk.Next = next
		}
	}
	jk.Missing = &missing
	switch {
	case jk.NoNext != "":
	case jk.Next.IsZero():
		jk.NoNext = "nothing to look up"
	default:
		// never right at startup, never twice in a row
		if start := since.Add(kindStartDelay); jk.Next.Before(start) {
			jk.Next = start
		}
		if jk.Last != nil && jk.Next.Before(jk.Last.Finished.Add(kindMinGap)) {
			jk.Next = jk.Last.Finished.Add(kindMinGap)
		}
	}
	return nil
}

func (k *localKinds) Pause(ctx context.Context, kind jobs.Kind, paused bool) (JobKind, error) {
	if _, err := ParseJobKind(kind.String()); err != nil {
		return JobKind{}, err
	}
	if err := k.update(kind, func(st *kindState) { st.Paused = paused }); err != nil {
		return JobKind{}, err
	}
	k.changed()
	return k.Get(ctx, kind)
}

func (k *localKinds) Set(ctx context.Context, kind jobs.Kind, s JobSettings) (JobKind, error) {
	jk, err := k.get(ctx, kind, false)
	if err != nil {
		return jk, err
	}
	set := func(key string, value any) error {
		_, err := k.core.Config.Set(ctx, key, value)
		return err
	}
	if s.Enabled != nil {
		var key string
		switch kind {
		case jobs.KindCheck:
			key = config.Jobs.Check.Enabled.Key
		case jobs.KindSourceCheck:
			key = config.Jobs.Sources.Enabled.Key
		case jobs.KindSourceURLs:
			key = config.Jobs.URLs.Enabled.Key
		default:
			return jk, invalid("%s is manual: it can't be turned off", jk.Title)
		}
		if err := set(key, *s.Enabled); err != nil {
			return jk, err
		}
	}
	if s.Schedule != nil {
		if jk.ScheduleKey == "" {
			return jk, invalid("%s is manual: it has no schedule", jk.Title)
		}
		if err := set(jk.ScheduleKey, strings.TrimSpace(*s.Schedule)); err != nil {
			return jk, err
		}
	}
	if len(s.Providers) > 0 {
		settings, key := providerSettings(kind)
		if key == "" {
			return jk, invalid("%s has no per-provider settings", jk.Title)
		}
		defaults := kindDefaults(kind)
		for id, v := range s.Providers {
			if v = strings.TrimSpace(v); v == "" {
				v = cmp.Or(defaults[id], config.ProviderOn)
			}
			settings[id] = v
		}
		// the prebuilt defaults stay out of the file
		for id, v := range settings {
			if v == cmp.Or(defaults[id], config.ProviderOn) {
				delete(settings, id)
			}
		}
		if err := set(key, settings); err != nil {
			return jk, err
		}
	}
	if s.DelayMin != nil {
		if err := set(config.Jobs.DelayMin.Key, strings.TrimSpace(*s.DelayMin)); err != nil {
			return jk, err
		}
	}
	if s.DelayMax != nil {
		if err := set(config.Jobs.DelayMax.Key, strings.TrimSpace(*s.DelayMax)); err != nil {
			return jk, err
		}
	}
	k.changed()
	return k.Get(ctx, kind)
}

func (k *localKinds) Run(ctx context.Context, kind jobs.Kind, all bool) (Job, error) {
	if _, err := ParseJobKind(kind.String()); err != nil {
		return Job{}, err
	}
	if k.m == nil {
		return Job{}, errors.New("no jobs here")
	}
	if j := k.active(kind); j != nil {
		return *j, fmt.Errorf("%w: %s runs (job %d)", ErrRefused, kind, j.ID)
	}
	var spec jobs.Spec
	var err error
	switch kind {
	case jobs.KindCheck:
		spec, err = k.checkSpec(ctx, all)
	case jobs.KindSourceCheck:
		spec, err = k.sourceCheckSpec(all)
	case jobs.KindSourceURLs:
		spec, err = k.urlsSpec(all)
	case jobs.KindScan:
		spec = k.scanSpec()
	case jobs.KindHealth:
		spec = k.healthSpec()
	}
	if err != nil {
		return Job{}, err
	}
	spec.Origin = cmp.Or(spec.Origin, k.opts.Origin)
	return JobOf(k.m.Add(spec)), nil
}

// record keeps the run as the kind's last, mark (may be nil) changes the rest.
func (k *localKinds) record(kind jobs.Kind, run JobRun, mark func(*kindState)) {
	_ = k.update(kind, func(st *kindState) {
		st.Last = &run
		if mark != nil {
			mark(st)
		}
	})
	k.changed()
}

func (k *localKinds) checkSpec(ctx context.Context, all bool) (jobs.Spec, error) {
	m, err := k.core.env.Watches(ctx)
	if err != nil {
		return jobs.Spec{}, err
	}
	list, err := m.List(ctx)
	if err != nil {
		return jobs.Spec{}, err
	}
	settings := config.Jobs.Check.Providers.Fresh()
	now := k.core.env.Now()
	var ids []int64
	var providers []string
	for _, w := range list {
		if !w.Enabled || !settings.On(w.Provider) || !all && (!w.Scheduled() || w.NextCheck.After(now)) {
			continue
		}
		ids = append(ids, w.ID)
		providers = append(providers, w.Provider)
	}
	if len(ids) == 0 {
		return jobs.Spec{}, fmt.Errorf("%w: no watch is due", ErrNothingToDo)
	}
	title := "check " + stringutil.Quantify(len(ids), "due watch", "due watches")
	if all {
		title = "check " + stringutil.Quantify(len(ids), "watch", "watches")
	}
	return jobs.CheckSpec(m, ids, providers, watch.CheckOptions{Kind: watch.KindManual}, title, k.opts.Origin, func(r watch.Report) string {
		return fmt.Sprintf("%s, %s, %s", stringutil.Quantify(r.Watches, "watch", "watches"),
			stringutil.Quantify(r.NewChapters, "new chapter", "new chapters"), stringutil.Quantify(r.Errors, "error", "errors"))
	}), nil
}

// picked are the sources a run looks up (all, or the due ones), a set by sourceKey.
func (k *localKinds) picked(kind jobs.Kind, all bool) (map[string]bool, error) {
	jk, err := k.get(context.Background(), kind, false)
	if err != nil {
		return nil, err
	}
	st, err := k.state(kind)
	if err != nil {
		return nil, err
	}
	settings, _ := providerSettings(kind)
	list, _, err := k.sources(kind, all, settings)
	if err != nil {
		return nil, err
	}
	interval, now := kindInterval(jk), k.core.env.Now()
	picked := map[string]bool{}
	for _, s := range list {
		if all || interval > 0 && !due(st, s, settings, interval, time.Time{}).After(now) {
			picked[sourceKey(s.provider, s.mangaID)] = true
		}
	}
	if len(picked) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNothingToDo, jk.MissingWhat)
	}
	return picked, nil
}

// markChecked records the sources as looked up at t.
func markChecked(keys []string, t time.Time) func(*kindState) {
	return func(st *kindState) {
		if st.Checked == nil {
			st.Checked = map[string]time.Time{}
		}
		for _, key := range keys {
			st.Checked[key] = t
		}
	}
}

func (k *localKinds) sourceCheckSpec(all bool) (jobs.Spec, error) {
	picked, err := k.picked(jobs.KindSourceCheck, all)
	if err != nil {
		return jobs.Spec{}, err
	}
	title := "source check: " + stringutil.Quantify(len(picked), "source", "sources")
	return jobs.Spec{Kind: jobs.KindSourceCheck, Title: title, Run: func(ctx context.Context, p *jobs.Progress) error {
		started := k.core.env.Now()
		var mu sync.Mutex
		var looked []string
		opts := health.Options{Network: true, Only: []string{"L11"},
			Sources: func(provider, mangaID string) bool { return picked[sourceKey(provider, mangaID)] },
			Looked: func(provider, mangaID string) {
				mu.Lock()
				defer mu.Unlock()
				looked = append(looked, sourceKey(provider, mangaID))
			}}
		report, err := k.runHealth(ctx, p, opts, started)
		run := JobRun{All: all, Started: started, Finished: k.core.env.Now()}
		if err != nil {
			run.Error = err.Error()
		} else {
			run.Summary = fmt.Sprintf("%d of %d looked up: %s gone", len(looked), len(picked), stringutil.Quantify(countCheck(report, "L11"), "source", "sources"))
			if n := len(report.Errors); n > 0 {
				run.Summary += ", " + stringutil.Quantify(n, "error", "errors")
			}
			// a finished run marks every source picked: one it couldn't ask (an old id, an
			// error) waits for its interval like the others
			looked = slices.Collect(maps.Keys(picked))
		}
		run.Run = k.healthRun(opts, started, report, err)
		run.Job = p.ID()
		p.Message(cmp.Or(run.Summary, run.Error))
		k.record(jobs.KindSourceCheck, run, markChecked(looked, started))
		return err
	}}, nil
}

// countCheck is how many findings the check has.
func countCheck(r health.Report, check string) int {
	n := 0
	for _, f := range r.Findings {
		if f.Check == check && f.Severity >= health.Warning {
			n++
		}
	}
	return n
}

// runHealth runs a health check, its progress the job's.
func (k *localKinds) runHealth(ctx context.Context, p *jobs.Progress, opts health.Options, _ time.Time) (health.Report, error) {
	p.Running("checking")
	ctx = progress.WithReporter(ctx, func(s progress.Status) {
		if !s.IsMinor() {
			p.Message(s.String())
		}
	})
	ctx = WithProgress(ctx, func(pr library.Progress) { p.Message(pr.String()) })
	return k.core.Health.Report(ctx, opts)
}

func (k *localKinds) healthRun(opts health.Options, started time.Time, report health.Report, err error) int64 {
	if k.opts.OnHealth == nil {
		return 0
	}
	return k.opts.OnHealth(opts, started, report, err)
}

func (k *localKinds) urlsSpec(all bool) (jobs.Spec, error) {
	picked, err := k.picked(jobs.KindSourceURLs, all)
	if err != nil {
		return jobs.Spec{}, err
	}
	title := "source addresses: " + stringutil.Quantify(len(picked), "source", "sources")
	return jobs.Spec{Kind: jobs.KindSourceURLs, Title: title, Run: func(ctx context.Context, p *jobs.Progress) error {
		started := k.core.env.Now()
		p.Running("looking up the source addresses")
		ctx = WithProgress(ctx, func(pr library.Progress) {
			p.Step(pr.Done, pr.Total)
			p.Message(pr.String())
		})
		rep, err := k.core.Library.LookupSourceURLs(ctx, SourceURLsOptions{All: all, Sources: func(provider, mangaID string) bool {
			return picked[sourceKey(provider, mangaID)] || picked[sourceKey(currentID(k.core, provider), mangaID)]
		}})
		looked := make([]string, 0, len(rep.Results))
		for _, r := range rep.Results {
			looked = append(looked, sourceKey(currentID(k.core, r.Provider), r.MangaID))
		}
		run := JobRun{Job: p.ID(), All: all, Started: started, Finished: k.core.env.Now(), Summary: SourceURLsSummary(rep)}
		if err != nil {
			run.Error = err.Error()
		} else {
			looked = slices.Collect(maps.Keys(picked))
		}
		p.Message(run.Summary)
		k.record(jobs.KindSourceURLs, run, markChecked(looked, started))
		return err
	}}, nil
}

// currentID is the provider's current id (an old one resolved), the id when it isn't
// installed.
func currentID(c *Core, id string) string {
	if loaders, err := c.env.Loaders(); err == nil {
		if l, _, ok := libmangal.FindLoader(loaders, id); ok {
			return l.Info().ID
		}
	}
	return id
}

func (k *localKinds) scanSpec() jobs.Spec {
	return jobs.Spec{Kind: jobs.KindScan, Title: "library scan", Run: func(ctx context.Context, p *jobs.Progress) error {
		started := k.core.env.Now()
		root, err := k.core.Library.Root(ctx)
		if err != nil {
			return err
		}
		p.Running("scanning " + root)
		ctx = WithProgress(ctx, func(pr library.Progress) { p.Message(pr.String()) })
		rep, err := k.core.Library.Scan(ctx, ScanOptions{})
		run := JobRun{Job: p.ID(), All: true, Started: started, Finished: k.core.env.Now()}
		if err != nil {
			run.Error = err.Error()
		} else {
			run.Summary = ScanSummary(rep)
			p.Message(run.Summary)
		}
		k.record(jobs.KindScan, run, nil)
		return err
	}}
}

func (k *localKinds) healthSpec() jobs.Spec {
	return jobs.Spec{Kind: jobs.KindHealth, Title: "network health check", Run: func(ctx context.Context, p *jobs.Progress) error {
		started := k.core.env.Now()
		opts := health.Options{Network: true}
		report, err := k.runHealth(ctx, p, opts, started)
		run := JobRun{Job: p.ID(), All: true, Started: started, Finished: k.core.env.Now()}
		if err != nil {
			run.Error = err.Error()
		} else {
			run.Summary = fmt.Sprintf("%d errors, %d warnings, %d info", report.Counts.Error, report.Counts.Warning, report.Counts.Info)
			p.Message(run.Summary)
		}
		run.Run = k.healthRun(opts, started, report, err)
		k.record(jobs.KindHealth, run, nil)
		return err
	}}
}

// SourceURLsSummary is what a source urls lookup found, in a line.
func SourceURLsSummary(rep SourceURLsReport) string {
	var found, gone, failed int
	for _, r := range rep.Results {
		switch {
		case r.Err == nil:
			found++
		case errors.Is(r.Err, ErrNotFound):
			gone++
		default:
			failed++
		}
	}
	s := fmt.Sprintf("%d found, %d gone, %d failed", found, gone, failed)
	if len(rep.Skipped) != 0 {
		s += "; skipped " + strings.Join(rep.Skipped, ", ")
	}
	return s
}

// ScanSummary is what a scan found, in a line.
func ScanSummary(rep ScanReport) string {
	r := rep.Result
	parts := []string{fmt.Sprintf("%d chapters added, %d moved, %d removed, %d new folders", r.Added, r.Moved, r.Removed, r.New)}
	if len(r.Missing) != 0 {
		parts = append(parts, fmt.Sprintf("%d missing", len(r.Missing)))
	}
	if len(rep.Duplicates) != 0 {
		parts = append(parts, fmt.Sprintf("%d duplicates", len(rep.Duplicates)))
	}
	return strings.Join(parts, ", ")
}

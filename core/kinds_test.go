package core

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/health"
	"github.com/luevano/mangal/v5/storage"
)

// kindWatches are watches for the watch checks kind.
type kindWatches struct {
	watch.Manager
	list []watch.Watch
}

func (m kindWatches) List(context.Context) ([]watch.Watch, error) { return m.list, nil }
func (m kindWatches) Runs(context.Context, watch.RunFilter) ([]watch.CheckRun, error) {
	return []watch.CheckRun{{ID: 7, Kind: watch.KindScheduled, Watches: 2, NewChapters: 1}}, nil
}
func (m kindWatches) Status(context.Context) (watch.Status, error) { return watch.Status{}, nil }

// waitJob waits for the manager's job to be over.
func waitJob(t *testing.T, m *jobs.Manager, id int64) jobs.Job {
	t.Helper()
	for {
		changed := m.Changed()
		if j, ok := m.Get(id); !ok || !j.Status.Active() {
			return j
		}
		select {
		case <-changed:
		case <-time.After(10 * time.Second):
			t.Fatalf("job %d didn't end", id)
		}
	}
}

func setConfig(t *testing.T, key string, value any) {
	t.Helper()
	old, _ := config.Get(key)
	if err := config.Set(key, value); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.Set(key, old) })
}

func TestJobKinds(t *testing.T) {
	c, site := urlCore(t, "mango-mangadex", "mango-mangapill", "mango-mangaplus")
	ctx := context.Background()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	started := now.Add(-time.Hour)
	c.env.Now = func() time.Time { return now }
	var mu sync.Mutex
	var pauses []time.Duration
	c.env.Sleep = func(_ context.Context, d time.Duration) error {
		mu.Lock()
		defer mu.Unlock()
		pauses = append(pauses, d)
		return nil
	}
	// a fixed pace: the injected sleep sees it
	setConfig(t, config.Jobs.DelayMin.Key, "5s")
	setConfig(t, config.Jobs.DelayMax.Key, "5s")
	addManga(t, c, "A", "mango-mangadex", "a", "A", 1)
	addManga(t, c, "B", "mango-mangadex", "b", "B", 1)
	addManga(t, c, "D", "mango-mangapill", "d", "D", 1)
	addManga(t, c, "P", "mango-mangaplus", "p", "P", 1)
	w := watch.Watch{Watch: storage.Watch{ID: 1, Provider: "mango-mangadex", Enabled: true}, NextCheck: now.Add(-time.Minute), NextScheduled: now.Add(time.Hour)}
	plus := watch.Watch{Watch: storage.Watch{ID: 2, Provider: "mango-mangaplus", Enabled: true}, NextCheck: now.Add(time.Hour), NextScheduled: now.Add(2 * time.Hour)}
	c.env.Watches = func(context.Context) (watch.Manager, error) { return kindWatches{list: []watch.Watch{w, plus}}, nil }
	m := jobs.New(ctx)
	defer m.Shutdown(time.Second)
	changes := 0
	kinds := c.Kinds(m, KindsOptions{Origin: "test", Started: started, Changed: func() { changes++ }})

	list, err := kinds.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, k := range list {
		names = append(names, k.Kind.String())
	}
	if !slices.Equal(names, []string{"check", "sources", "urls", "scan", "health"}) {
		t.Fatalf("kinds %q", names)
	}
	if !list[3].Manual || !list[4].Manual || list[3].Next.IsZero() == false {
		t.Errorf("scan, health %+v %+v", list[3], list[4])
	}

	// watch checks: MangaPlus stays on (watches are chosen), the next is the next scheduled
	check, err := kinds.Get(ctx, jobs.KindCheck)
	if err != nil {
		t.Fatal(err)
	}
	if !check.Next.Equal(w.NextScheduled) || *check.Missing != 1 || check.Last == nil || check.Last.Run != 7 {
		t.Errorf("check %+v", check)
	}
	if i := slices.IndexFunc(check.Providers, func(p JobProvider) bool { return p.Provider == "mango-mangaplus" }); i < 0 || !check.Providers[i].On {
		t.Errorf("check providers %+v", check.Providers)
	}

	// source addresses: MangaPlus off by default, never run: an interval after the start
	urls, err := kinds.Get(ctx, jobs.KindSourceURLs)
	if err != nil {
		t.Fatal(err)
	}
	if want := started.Add(168 * time.Hour); !urls.Next.Equal(want) || *urls.Missing != 3 {
		t.Errorf("urls next %s missing %d, want %s 3", urls.Next, *urls.Missing, want)
	}
	i := slices.IndexFunc(urls.Providers, func(p JobProvider) bool { return p.Provider == "mango-mangaplus" })
	if i < 0 || urls.Providers[i].On || urls.Providers[i].Default != config.ProviderOff || urls.Providers[i].Items != 0 {
		t.Errorf("urls providers %+v", urls.Providers)
	}

	// a provider's own interval
	setConfig(t, config.Jobs.URLs.Providers.Key, map[string]any{"mango-mangadex": "24h"})
	if urls, _ = kinds.Get(ctx, jobs.KindSourceURLs); !urls.Next.Equal(started.Add(24 * time.Hour)) {
		t.Errorf("override: next %s", urls.Next)
	}

	// paused: no next run, a run still works
	if urls, err = kinds.Pause(ctx, jobs.KindSourceURLs, true); err != nil || !urls.Paused || !urls.Next.IsZero() || urls.NoNext != "paused" || changes != 1 {
		t.Errorf("paused %+v %v", urls, err)
	}

	job, err := kinds.Run(ctx, jobs.KindSourceURLs, false)
	if err != nil {
		t.Fatal(err)
	}
	if j := waitJob(t, m, job.ID); j.Status != jobs.Done || j.Kind != jobs.KindSourceURLs {
		t.Fatalf("urls job %+v", j)
	}
	if slices.Contains(site.asked, "mango-mangaplus p") || len(site.asked) != 3 {
		t.Errorf("asked %q", site.asked)
	}
	mu.Lock()
	if len(pauses) != 1 || pauses[0] != 5*time.Second {
		t.Errorf("pauses %v", pauses)
	}
	mu.Unlock()
	urls, _ = kinds.Pause(ctx, jobs.KindSourceURLs, false)
	if urls.Last == nil || urls.Last.Job != job.ID || urls.Last.Summary != "3 found, 0 gone, 0 failed" || *urls.Missing != 0 || urls.NoNext != "nothing to look up" {
		t.Errorf("after the run %+v %+v", urls, urls.Last)
	}
	if _, err := kinds.Run(ctx, jobs.KindSourceURLs, false); !errors.Is(err, ErrNothingToDo) {
		t.Errorf("nothing missing: %v", err)
	}

	// source checks: a provider turned on, the lookups through the health checker
	var looked []string
	c.env.Checker = func(m watch.Manager) (health.Checker, error) {
		loaders, _ := c.env.Loaders()
		return health.Checker{Library: c.env.Storage.Library(), Watches: m, Loaders: loaders,
			Lookup: func(_ context.Context, provider string, ids []string, done func(int, bool, error)) error {
				for i, id := range ids {
					looked = append(looked, provider+" "+id)
					done(i, id != "b", nil)
				}
				return nil
			}}, nil
	}
	var reported []health.Options
	kinds = c.Kinds(m, KindsOptions{Started: started, OnHealth: func(opts health.Options, _ time.Time, _ health.Report, _ error) int64 {
		reported = append(reported, opts)
		return 3
	}})
	if _, err := kinds.Set(ctx, jobs.KindSourceCheck, JobSettings{Providers: map[string]string{"mango-mangaplus": "on", "mango-mangapill": "off"}}); err != nil {
		t.Fatal(err)
	}
	job, err = kinds.Run(ctx, jobs.KindSourceCheck, true)
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, m, job.ID)
	slices.Sort(looked)
	if !slices.Equal(looked, []string{"mango-mangadex a", "mango-mangadex b", "mango-mangaplus p"}) {
		t.Errorf("looked up %q", looked)
	}
	src, _ := kinds.Get(ctx, jobs.KindSourceCheck)
	if src.Last == nil || src.Last.Run != 3 || src.Last.Summary != "3 of 3 looked up: 1 source gone" || len(reported) != 1 {
		t.Errorf("source check %+v", src.Last)
	}
	if *src.Missing != 0 || !src.Next.Equal(now.Add(168*time.Hour)) {
		t.Errorf("source check next %s missing %d", src.Next, *src.Missing)
	}
	// back to the defaults: MangaPlus off again, the file doesn't list them
	if _, err := kinds.Set(ctx, jobs.KindSourceCheck, JobSettings{Providers: map[string]string{"mango-mangaplus": "", "mango-mangapill": ""}}); err != nil {
		t.Fatal(err)
	}
	if p := config.Jobs.Sources.Providers.Get(); p.On("mango-mangaplus") || !p.On("mango-mangapill") {
		t.Errorf("reset %v", p)
	}
	if _, err := kinds.Set(ctx, jobs.KindScan, JobSettings{Enabled: new(false)}); !errors.Is(err, ErrInvalid) {
		t.Errorf("scan off: %v", err)
	}
	if _, err := ParseJobKind("download"); !errors.Is(err, ErrNotFound) {
		t.Errorf("download is no kind: %v", err)
	}
}

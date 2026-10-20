package serve

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/health"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

type fakeLoader struct{}

func (fakeLoader) String() string { return "fake" }
func (fakeLoader) Info() libmangal.ProviderInfo {
	return libmangal.ProviderInfo{ID: "fake", Name: "fake"}
}
func (fakeLoader) Load(context.Context) (libmangal.Provider, error) { return nil, nil }

type alMeta struct {
	metadata.Metadata
	id, title string
}

func (m alMeta) Title() string { return m.title }
func (m alMeta) ID() metadata.ID {
	return metadata.ID{Raw: m.id, Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}
}

// newHealthServer is a test server whose library has One-Punch Man with chapter 3 missing
// (the fake provider has it), not linked to AniList (a search finds it); probes wait for
// gate.
func newHealthServer(t *testing.T) (*testServer, chan struct{}, *[]string) {
	s := newTestServer(t, "")
	for _, n := range []float32{1, 2, 4} {
		if err := s.src.library.RecordDownload("/lib", "One-Punch Man", storage.Download{Path: fmt.Sprintf("/lib/One-Punch Man/%g.cbz", n),
			Provider: "fake", MangaID: "opm", MangaTitle: "One-Punch Man", Chapter: n, Format: "CBZ"}); err != nil {
			t.Fatal(err)
		}
	}
	gate := make(chan struct{})
	var ops []string
	s.api.Health = func(m watch.Manager) (health.Checker, error) {
		return health.Checker{
			Library: s.src.library,
			Watches: m,
			Loaders: []libmangal.ProviderLoader{fakeLoader{}},
			Probe: func(ctx context.Context, provider string, _ health.ProviderManga) health.Probe {
				select {
				case <-gate:
				case <-ctx.Done():
				}
				return health.Probe{Took: time.Second}
			},
			Chapters: func(_ context.Context, _ string, mangas []health.ProviderManga, _ func(string), done func(int, health.ProviderChapters, error)) error {
				for i := range mangas {
					done(i, health.ProviderChapters{Listed: []float32{1, 2, 3, 4}}, nil)
				}
				return nil
			},
			SearchAnilist: func(_ context.Context, queries []metadata.Query) ([]metadata.Match, []bool, error) {
				return []metadata.Match{{Metadata: alMeta{id: "74347", title: "One-Punch Man"}, Score: 1}}, []bool{true}, nil
			},
		}, nil
	}
	backups := t.TempDir()
	cacheDB, err := storage.OpenCache(filepath.Join(t.TempDir(), storage.FilenameCache))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cacheDB.Close() })
	s.api.Core = func(m watch.Manager) (*core.Core, error) {
		return core.New(context.Background(), core.Env{
			Storage:    storage.Storage{Data: s.src.library.DB, Cache: cacheDB},
			FS:         afero.NewMemMapFs(),
			Root:       func() string { return "/lib" },
			Modes:      func() library.Modes { return library.DefaultModes },
			BackupsDir: backups,
			Loaders:    func() ([]libmangal.ProviderLoader, error) { return []libmangal.ProviderLoader{fakeLoader{}}, nil },
			Watches:    func(context.Context) (watch.Manager, error) { return m, nil },
			Checker:    s.api.Health,
			Metadata:   func(context.Context, []metadata.ID) (metadata.Metadata, error) { return nil, nil },
			CheckLink: func(context.Context, metadata.IDCode, string) (metadata.Metadata, error) {
				ops = append(ops, "check")
				return nil, nil
			},
		})
	}
	return s, gate, &ops
}

func TestHealthOffline(t *testing.T) {
	s, _, _ := newHealthServer(t)
	var report health.Report
	if code := s.call(t, "GET", "/api/health", "", &report); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if _, ok := report.Find("G1/manga/1"); !ok || report.Network {
		t.Errorf("report %+v", report)
	}
	if _, ok := report.Find("G2/manga/1"); ok {
		t.Error("a network finding offline")
	}
	var only health.Report
	if code := s.call(t, "GET", "/api/health?only=gaps", "", &only); code != http.StatusOK || len(only.Findings) != 1 {
		t.Errorf("only gaps: %d %+v", code, only)
	}
	if code := s.call(t, "GET", "/api/health?only=nope", "", nil); code != http.StatusBadRequest {
		t.Errorf("unknown check: %d", code)
	}
}

func TestHealthNetworkJob(t *testing.T) {
	s, gate, ops := newHealthServer(t)
	var job HealthJob
	if code := s.call(t, "GET", "/api/health?network=1", "", &job); code != http.StatusAccepted || !job.Running || job.ID != 1 {
		t.Fatalf("job %d %+v", code, job)
	}
	// one at a time: asked again, the running one
	var again HealthJob
	if code := s.call(t, "GET", "/api/health?network=true", "", &again); code != http.StatusAccepted || again.ID != 1 {
		t.Errorf("again %d %+v", code, again)
	}
	close(gate)
	deadline := time.Now().Add(10 * time.Second)
	for job.Running && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		s.call(t, "GET", "/api/health/jobs/1", "", &job)
	}
	if job.Running || job.Report == nil || job.Error != "" {
		t.Fatalf("job %+v", job)
	}
	g2, ok := job.Report.Find("G2/manga/1")
	if !ok || !strings.Contains(g2.Message, "1 of 1 missing chapter on fake, downloadable: 3") {
		t.Errorf("G2 %+v", g2)
	}
	if a2, ok := job.Report.Find("A2/providers"); !ok || a2.Message != "1 of 1 provider answered" {
		t.Errorf("A2 %+v", a2)
	}
	// its events, kept for later subscribers
	req, _ := http.NewRequest("GET", s.URL+"/api/events", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var kinds []string
	readSSE(t, res.Body, func(_ string, e watch.LiveEvent) bool {
		if e.Job != 1 {
			t.Errorf("event %+v", e)
		}
		kinds = append(kinds, e.Kind)
		return e.Kind == watch.LiveHealthDone
	})
	if kinds[0] != watch.LiveHealthStart || !slices.Contains(kinds, watch.LiveHealthStatus) || kinds[len(kinds)-1] != watch.LiveHealthDone {
		t.Errorf("kinds %v", kinds)
	}
	if code := s.call(t, "GET", "/api/health/jobs/9", "", nil); code != http.StatusNotFound {
		t.Errorf("unknown job: %d", code)
	}

	// M1's AniList match comes from the job's report: plan, then apply
	var plan health.FixResult
	if code := s.call(t, "POST", "/api/health/fix", `{"key":"M1/manga/1"}`, &plan); code != http.StatusOK || plan.Applied || len(plan.Plan) == 0 || plan.Kind != health.ActionLink {
		t.Fatalf("plan %d %+v", code, plan)
	}
	var applied health.FixResult
	if code := s.call(t, "POST", "/api/health/fix", `{"key":"M1/manga/1","apply":true}`, &applied); code != http.StatusOK || !applied.Applied || applied.Snapshot == "" || len(*ops) == 0 {
		t.Fatalf("apply %d %+v %q", code, applied, *ops)
	}
	if op, _, _ := (storage.Journal{DB: s.src.library.DB}).Last(); op.Kind != "link" {
		t.Errorf("last operation %+v", op)
	}
	m, _, _ := s.src.library.MangaByID(1)
	if !slices.ContainsFunc(m.MetadataIDs, func(id metadata.ID) bool { return id.Raw == "74347" }) {
		t.Errorf("not linked: %+v", m.MetadataIDs)
	}
	for body, want := range map[string]int{
		`{"key":"G1/manga/1"}`:          http.StatusUnprocessableEntity, // no fix to apply here
		`{"key":"G1/manga/99"}`:         http.StatusNotFound,
		`{"key":"bogus"}`:               http.StatusBadRequest,
		`{"key":"M1/manga/1","nope":1}`: http.StatusBadRequest,
	} {
		if code := s.call(t, "POST", "/api/health/fix", body, nil); code != want {
			t.Errorf("%s: %d, want %d", body, code, want)
		}
	}
}

// fakeKinds are job kinds whose next run and runs a test sets.
type fakeKinds struct {
	core.JobKinds
	next time.Time
	run  error
	ran  []bool
}

func (k *fakeKinds) Get(_ context.Context, kind jobs.Kind) (core.JobKind, error) {
	return core.JobKind{Kind: kind, Title: "Source checks", Next: k.next}, nil
}

func (k *fakeKinds) Run(_ context.Context, kind jobs.Kind, all bool) (core.Job, error) {
	k.ran = append(k.ran, all)
	return core.Job{ID: 4, Kind: kind, Title: "source check: 2 sources"}, k.run
}

// TestKindSchedule: a scheduled kind waits for its next run (looking again every
// kindPoll), then runs its missing ones and waits for the job; a run refused is tried
// again kindRetry later, nothing to do waits a poll.
func TestKindSchedule(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	k := &fakeKinds{}
	waited := 0
	s := kindSchedule{Kind: jobs.KindSourceCheck, Kinds: func(context.Context) (core.JobKinds, error) { return k, nil },
		Wait: func(context.Context, int64) core.Job { waited++; return core.Job{Message: "3 of 3 looked up"} },
		Now:  func() time.Time { return now }, Log: newLogger(io.Discard)}
	ctx := context.Background()
	for _, c := range []struct {
		next time.Time
		run  error
		want time.Duration
		ran  int
	}{
		{time.Time{}, nil, kindPoll, 0},
		{now.Add(time.Minute), nil, time.Minute, 0},
		{now.Add(time.Hour), nil, kindPoll, 0},
		{now, nil, 0, 1},
		{now.Add(-time.Hour), core.ErrRefused, kindRetry, 1},
		{now, core.ErrNothingToDo, kindPoll, 1},
	} {
		k.next, k.run, k.ran, waited = c.next, c.run, nil, 0
		if got := s.step(ctx); got != c.want || len(k.ran) != c.ran || slices.Contains(k.ran, true) {
			t.Errorf("next %s, run %v: waits %s, ran %v; want %s, %d runs", c.next, c.run, got, k.ran, c.want, c.ran)
		}
		wantWaits := 0
		if c.run == nil && c.ran == 1 {
			wantWaits = 1
		}
		if waited != wantWaits {
			t.Errorf("next %s: waited for %d jobs, want %d", c.next, waited, wantWaits)
		}
	}
}

// TestSourceCheckRun: a source check job is kept as a health job; its L11 findings are in
// the offline report while they hold, and can be dismissed from it.
func TestSourceCheckRun(t *testing.T) {
	s, _, _ := newHealthServer(t)
	inner := s.api.Health
	s.api.Health = func(m watch.Manager) (health.Checker, error) {
		c, err := inner(m)
		c.Ignores = storage.HealthIgnores{DB: s.src.library.DB}.List
		c.Lookup = func(_ context.Context, _ string, ids []string, done func(int, bool, error)) error {
			for i := range ids {
				done(i, false, nil)
			}
			return nil
		}
		return c, err
	}
	m := jobs.New(context.Background())
	defer m.Shutdown(time.Second)
	s.api.Jobs = m
	kinds, err := s.api.kinds(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	run, err := kinds.Run(context.Background(), jobs.KindSourceCheck, true)
	if err != nil {
		t.Fatal(err)
	}
	if j := waitJob(context.Background(), m, run.ID); j.Status != jobs.Done {
		t.Fatalf("source check job %+v", j)
	}
	job, ok := s.api.healthJobs.get(1)
	if !ok || job.Running || job.Report == nil {
		t.Fatalf("job %+v", job)
	}
	const key = "L11/manga/1/fake/opm"
	if f, ok := job.Report.Find(key); !ok || f.Message != "not on fake any more (it may have been removed)" {
		t.Errorf("job's L11 %+v", job.Report.Findings)
	}
	var report health.Report
	if code := s.call(t, "GET", "/api/v1/health", "", &report); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if _, ok := report.Find(key); !ok || report.Network {
		t.Errorf("offline report %+v", report.Findings)
	}
	if code := s.call(t, "GET", "/api/v1/health?only=gaps", "", &report); code != http.StatusOK {
		t.Fatalf("status %d", code)
	} else if _, ok := report.Find(key); ok {
		t.Error("L11 with only=gaps")
	}
	if code := s.call(t, "POST", "/api/v1/health/dismiss", `{"dismiss":["`+key+`"]}`, nil); code != http.StatusOK {
		t.Fatalf("dismiss %d", code)
	}
	report = health.Report{}
	if s.call(t, "GET", "/api/v1/health", "", &report); report.Dismissed == 0 {
		t.Errorf("not dismissed: %+v", report)
	} else if _, ok := report.Find(key); ok {
		t.Error("dismissed, still there")
	}
}

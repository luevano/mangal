package jobs

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/jobs"
)

// fakeServe is a mangal serve's jobs, what's asked is kept.
type fakeServe struct {
	core.Jobs
	asked []core.DownloadReq
	list  []core.Job
}

func (f *fakeServe) Download(_ context.Context, req core.DownloadReq) (core.Job, error) {
	f.asked = append(f.asked, req)
	return core.Job{ID: 7, Kind: jobs.KindDownload, Status: jobs.Running, Origin: req.Origin}, nil
}

func (f *fakeServe) Get(context.Context, int64) (core.Job, error) { return core.Job{ID: 7}, nil }
func (f *fakeServe) Active() []core.Job                           { return f.list }

// the downloads are serve's, reading stays here; a job of one is never taken for the other's
func TestServed(t *testing.T) {
	m := jobs.New(context.Background())
	t.Cleanup(func() { m.Shutdown(time.Second) })
	serve := &fakeServe{}
	svc := served{local: core.LocalJobs(m), serve: serve}
	ctx := context.Background()

	j, err := svc.Download(ctx, core.DownloadReq{Provider: "p", MangaID: "m", Origin: "chapters"})
	if err != nil || j.ID != -7 || len(serve.asked) != 1 {
		t.Fatalf("a download: %+v, %v, %d asked", j, err, len(serve.asked))
	}
	if j, err := svc.Get(ctx, j.ID); err != nil || j.ID != -7 {
		t.Errorf("serve's job: %+v, %v", j, err)
	}
	if _, err := svc.Download(ctx, core.DownloadReq{Provider: "p", MangaID: "m", Read: true}); err == nil && len(serve.asked) != 1 {
		t.Errorf("a download for reading went to serve: %+v", serve.asked)
	}
	if len(serve.asked) != 1 {
		t.Errorf("a download for reading went to serve: %+v", serve.asked)
	}
	local := m.Add(jobs.Spec{Kind: jobs.KindCheck, Title: "check"})
	if got, err := svc.Get(ctx, local.ID); err != nil || got.ID != local.ID {
		t.Errorf("the manager's job: %+v, %v", got, err)
	}
}

// quitting says what goes on in serve, what this TUI started only, and it's not what's cancelled
func TestServedQuit(t *testing.T) {
	serve := &fakeServe{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := StartServed(ctx, serve).(*activity)
	t.Cleanup(func() { a.Shutdown(time.Second); SetManager(nil); mu.Lock(); remote = nil; mu.Unlock() })
	if got := a.Continuing(); got != "" {
		t.Errorf("nothing running: %q", got)
	}
	serve.list = []core.Job{
		{ID: 1, Kind: jobs.KindDownload, Status: jobs.Running, Origin: "chapters"},
		{ID: 2, Kind: jobs.KindDownload, Status: jobs.Queued, Origin: "chapters"},
		{ID: 3, Kind: jobs.KindDownload, Status: jobs.Running},
		{ID: 4, Kind: jobs.KindCheck, Status: jobs.Running, Origin: "watches"},
	}
	if got := a.Continuing(); got != "2 downloads continue in mangal serve" {
		t.Errorf("%q", got)
	}
	if running := a.Running(); len(running) != 0 {
		t.Errorf("serve's jobs would be cancelled: %v", running)
	}
	if !strings.Contains(a.Status(), "3 running") {
		t.Errorf("the header: %q", a.Status())
	}
}

func TestNotServedSaysNothing(t *testing.T) {
	a := &activity{svc: &fakeServe{list: []core.Job{{ID: 1, Kind: jobs.KindDownload, Status: jobs.Running, Origin: "x"}}}, remote: true}
	if got := a.Continuing(); got != "" {
		t.Errorf("a server's: %q", got)
	}
}

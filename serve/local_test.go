package serve

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/script/lib/api"
	lua "github.com/yuin/gopher-lua"
)

// newTestLocal is the in-process API over the health server's library with perms.
func newTestLocal(t *testing.T, perms ...string) (*Local, *jobs.Manager) {
	t.Helper()
	s, _, _ := newHealthServer(t)
	m := jobs.New(context.Background())
	s.api.Jobs = m
	scopes, err := NormalizeScopes(perms)
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	l, err := newLocal(ctx, stop, s.api, scopes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close(context.Background()) })
	return l, m
}

func runLocal(ctx context.Context, l *Local, script string) error {
	state := lua.NewState()
	defer state.Close()
	state.SetContext(ctx)
	connect := func(context.Context, string, string) (api.Conn, error) {
		return api.Conn{URL: LocalURL, HTTP: l.Client}, nil
	}
	state.PreloadModule("api", api.Lib(connect).Loader())
	return state.DoString(script)
}

// TestLocal: mangal.api over the in-process API: reads, a plan applied with its hash, a
// job waited for, the events, as the script identity.
func TestLocal(t *testing.T) {
	l, m := newTestLocal(t, ScopeManage)
	release := make(chan struct{})
	job := m.Add(jobs.Spec{Kind: jobs.KindDownload, Title: "small", Providers: []string{"fake"}, MangaID: "opm",
		Run: func(ctx context.Context, p *jobs.Progress) error {
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}})
	time.AfterFunc(100*time.Millisecond, func() { close(release) })
	err := runLocal(context.Background(), l, fmt.Sprintf(`
local api = require("api")
assert(api.status().version == "test")
local list = api.get("/library/mangas")
assert(list.mangas[1].title == "One-Punch Man", list.mangas[1].title)
local plan = api.change("library/forget", { mangas = { 1 } })
assert(string.find(plan.hash, "^sha256:") and #plan.lines > 0)
local ok, e = pcall(api.apply, "library/forget", { mangas = { 1 } }, "sha256:stale")
assert(not ok and e.status == 409 and e.code == "plan_changed" and e.plan.hash == plan.hash, tostring(e))
local res = api.apply("library/forget", { mangas = { 1 } }, plan)
assert(res.applied and res.result.op > 0 and res.undo)
local job = api.wait_job(%d)
assert(job.status == "done", job.status)
local seen
api.events({ "job" }, function(ev) seen = ev; return false end)
assert(seen.topic == "job")
`, job.ID))
	if err != nil {
		t.Fatal(err)
	}
}

// TestLocalRefused: without the permission the API refuses, as a table error.
func TestLocalRefused(t *testing.T) {
	l, _ := newTestLocal(t, ScopeRead, ScopeProgress, ScopeBrowse)
	err := runLocal(context.Background(), l, `
local api = require("api")
local ok, e = pcall(api.change, "library/forget", { mangas = { 1 } })
assert(not ok and e.status == 403 and e.code == "forbidden", tostring(e))
assert(string.find(e.message, "script"), e.message)
`)
	if err != nil {
		t.Fatal(err)
	}
}

// TestLocalCancel: a cancelled script stops waiting for its events; Close with a cancelled
// context cancels the jobs left.
func TestLocalCancel(t *testing.T) {
	l, m := newTestLocal(t, ScopeManage)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := runLocal(ctx, l, `require("api").events({ "job" }, function() return true end)`)
	if err == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("events: %v", err)
	}

	job := m.Add(jobs.Spec{Kind: jobs.KindDownload, Title: "long", Providers: []string{"fake"}, MangaID: "dan",
		Run: func(ctx context.Context, p *jobs.Progress) error {
			<-ctx.Done()
			return ctx.Err()
		}})
	if l.Active() != 1 {
		t.Errorf("active %d", l.Active())
	}
	done, stop := context.WithCancel(context.Background())
	stop()
	l.Close(done)
	if j, _ := m.Get(job.ID); j.Status.Active() {
		t.Errorf("job %s after close", j.Status)
	}
}

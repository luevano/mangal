package serve

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/core/remote"
	"github.com/luevano/mangal/v5/health"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
)

// TestRemoteChanges: core/remote's Changes over this API plan, apply and undo as the local
// ones, with the details as their Go types and the errors as core's.
func TestRemoteChanges(t *testing.T) {
	s, _, _ := newHealthServer(t)
	ctx := t.Context()
	rc, err := remote.New(remote.Options{URL: s.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rc.Status(ctx); err != nil {
		t.Fatal(err)
	}
	ch := rc.Changes()

	plan, err := ch.Plan(ctx, core.ForgetReq{Mangas: []int64{1}})
	if err != nil {
		t.Fatal(err)
	}
	if mangas, ok := plan.Detail.([]storage.Manga); !ok || len(mangas) != 1 || mangas[0].ID != 1 {
		t.Fatalf("forget detail %#v", plan.Detail)
	}
	// the library changes: the plan is stale
	if err := s.src.library.RecordDownload("/lib", "One-Punch Man", storage.Download{Path: "/lib/One-Punch Man/5.cbz",
		Provider: "fake", MangaID: "opm", MangaTitle: "One-Punch Man", Chapter: 5, Format: "CBZ"}); err != nil {
		t.Fatal(err)
	}
	_, err = ch.ApplyPlan(ctx, plan, core.ApplyOpts{})
	var changed *core.PlanChangedError
	if !errors.As(err, &changed) || changed.Plan.Change == nil || changed.Plan.Hash == plan.Hash {
		t.Fatalf("stale plan: %v", err)
	}
	var lines []core.Line
	res, err := ch.ApplyPlan(ctx, changed.Plan, core.ApplyOpts{OnLine: func(l core.Line) { lines = append(lines, l) }})
	if err != nil || res.OpID == 0 || len(lines) != len(res.Lines) {
		t.Fatalf("apply %+v %v", res, err)
	}
	ops, err := rc.Ops(ctx, 1)
	if err != nil || len(ops) != 1 || ops[0].ID != res.OpID || ops[0].Kind != "remove" {
		t.Fatalf("ops %+v %v", ops, err)
	}

	undo, err := ch.Plan(ctx, core.UndoReq{})
	if err != nil {
		t.Fatal(err)
	}
	if up, ok := undo.Detail.(library.UndoPlan); !ok || up.Op.ID != res.OpID {
		t.Fatalf("undo detail %#v", undo.Detail)
	}
	done, err := ch.ApplyPlan(ctx, undo, core.ApplyOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := done.Detail.(core.UndoDone); !ok {
		t.Fatalf("undone detail %#v", done.Detail)
	}
	if _, err := ch.Plan(ctx, core.UndoReq{}); !errors.Is(err, library.ErrNothingToUndo) {
		t.Errorf("nothing to undo: %v", err)
	}
	if code := s.call(t, "POST", "/api/v1/ops/0/undo", "", nil); code != http.StatusBadRequest {
		t.Errorf("undo of op 0: %d", code)
	}

	if _, err := ch.Plan(ctx, core.MergeReq{Keep: 1, Other: 999}); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("merge with a missing manga: %v", err)
	}
	if _, err := ch.Plan(ctx, core.MoveReq{Manga: 1}); !errors.Is(err, core.ErrInvalid) {
		t.Errorf("move: %v", err)
	}
	bad, err := remote.New(remote.Options{URL: s.URL, Token: "wrong"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bad.Status(ctx); !errors.Is(err, remote.ErrUnauthorized) {
		t.Errorf("a wrong token: %v", err)
	}
}

// TestRemoteServices: core/remote's read services over this API answer as the local ones,
// and what the API doesn't have is errors.ErrUnsupported.
func TestRemoteServices(t *testing.T) {
	s, _, _ := newHealthServer(t)
	m := jobs.New(context.Background())
	defer m.Shutdown(time.Second)
	s.api.Jobs = m
	ctx := t.Context()
	rc, err := remote.New(remote.Options{URL: s.URL})
	if err != nil {
		t.Fatal(err)
	}
	svc := rc.Services()

	list, err := svc.Library.Mangas(ctx, core.MangasOptions{})
	if err != nil || len(list.Mangas) != 1 {
		t.Fatalf("mangas %+v %v", list, err)
	}
	opm := list.Mangas[0]
	if opm.Title != "One-Punch Man" || opm.Path() != "/lib/One-Punch Man" || len(opm.Sources) != 1 || opm.Sources[0].MangaID != "opm" {
		t.Errorf("manga %+v", opm)
	}
	if m, err := svc.Library.Manga(ctx, opm.ID); err != nil || m.Title != opm.Title {
		t.Errorf("manga %d: %+v %v", opm.ID, m, err)
	}
	if _, err := svc.Library.Manga(ctx, 999); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("missing manga: %v", err)
	}
	if m, ok, err := svc.Library.MangaBySource(ctx, "fake", "opm"); err != nil || !ok || m.ID != opm.ID {
		t.Errorf("by source %+v %v %v", m, ok, err)
	}
	downloads, err := svc.Library.Downloads(ctx, opm.ID)
	if err != nil || len(downloads) != 3 || downloads[2].Chapter != 4 {
		t.Errorf("downloads %+v %v", downloads, err)
	}
	if pd, err := svc.Library.ProviderDownloads(ctx, "fake", "opm"); err != nil || len(pd) != 3 || pd[0].Chapter != 1 {
		t.Errorf("provider downloads %+v %v", pd, err)
	}
	if _, err := svc.Library.Trash(ctx); err != nil {
		t.Errorf("trash: %v", err)
	}
	if _, err := svc.Library.Duplicates(ctx); err != nil {
		t.Errorf("duplicates: %v", err)
	}
	if _, err := svc.Bindings.OfManga(ctx, opm.ID); err != nil {
		t.Errorf("bindings: %v", err)
	}
	// the server's scan job runs (its library has no folder)
	if _, err := svc.Library.Scan(ctx, core.ScanOptions{}); err == nil || !strings.Contains(err.Error(), "drive not mounted") {
		t.Errorf("scan: %v", err)
	}
	if _, err := svc.Library.Links(ctx, opm.ID); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("links: %v", err)
	}
	if report, err := svc.Health.Report(ctx, health.Options{}); err != nil || report.Network {
		t.Errorf("health %+v %v", report, err)
	}
	if providers, err := svc.Providers.List(ctx); err != nil || len(providers) != 1 || providers[0].ID != "fake" {
		t.Errorf("providers %+v %v", providers, err)
	}
	if entries, err := svc.Config.List(ctx); err != nil || len(entries) == 0 {
		t.Errorf("config: %d %v", len(entries), err)
	}
	if e, err := svc.Config.Get(ctx, "download.format"); err != nil || e.Key != "download.format" {
		t.Errorf("config key %+v %v", e, err)
	}
}

// TestRemoteJobs: core/remote's Jobs list, cancel and remove the server's jobs, their
// events keep the marks' cache.
func TestRemoteJobs(t *testing.T) {
	s, _, _ := newHealthServer(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	m := jobs.New(context.Background())
	defer m.Shutdown(time.Second)
	s.api.Jobs = m
	go s.api.Events.forwardJobs(ctx, m)
	rc, err := remote.New(remote.Options{URL: s.URL})
	if err != nil {
		t.Fatal(err)
	}
	rj := rc.Jobs()
	events, err := rj.Subscribe(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	// the stream is open once the server's hub has its subscriber
	for deadline := time.Now().Add(5 * time.Second); subscribers(s.api.Events) == 0 && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	release := make(chan struct{})
	job := m.Add(jobs.Spec{Kind: jobs.KindDownload, Title: "wait", Providers: []string{"fake"}, MangaID: "opm",
		Run: func(ctx context.Context, p *jobs.Progress) error {
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		}})
	select {
	case e := <-events:
		if e.Kind != core.JobAdded || e.Job.ID != job.ID {
			t.Errorf("event %+v", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no added event")
	}
	if _, ok := rj.MangaStatus("fake", "opm"); !ok {
		t.Error("no mark of the active download")
	}
	if got, err := rj.Get(ctx, job.ID); err != nil || got.Title != "wait" {
		t.Errorf("get %+v %v", got, err)
	}
	if ok, err := rj.Remove(ctx, job.ID); err != nil || ok {
		t.Errorf("remove an active job: %v %v", ok, err)
	}
	if ok, err := rj.Cancel(ctx, job.ID); err != nil || !ok {
		t.Fatalf("cancel: %v %v", ok, err)
	}
	for e := range events {
		if e.Kind == core.JobFinished && e.Job.ID == job.ID {
			break
		}
	}
	if _, ok := rj.MangaStatus("fake", "opm"); ok {
		t.Error("a mark after the job ended")
	}
	if ok, err := rj.Cancel(ctx, job.ID); err != nil || ok {
		t.Errorf("cancel again: %v %v", ok, err)
	}
	if ok, err := rj.Remove(ctx, job.ID); err != nil || !ok {
		t.Errorf("remove: %v %v", ok, err)
	}
	if _, err := rj.Download(ctx, core.DownloadReq{Provider: "nope", MangaID: "x", ChapterRefs: []core.ChapterRef{{Number: 1}}}); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("download from a missing provider: %v", err)
	}
	close(release)
}

func subscribers(h *Events) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

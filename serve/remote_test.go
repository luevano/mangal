package serve

import (
	"errors"
	"net/http"
	"testing"

	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/remote"
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

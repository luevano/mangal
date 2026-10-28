package remote

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// busyRetry is how often an apply is tried again while the server's library is busy.
var busyRetry = 2 * time.Second

// Changes are the server's: each change is planned and applied by its API endpoint (move
// and retitle aren't yet: the provider's manga is found locally). A plan's Change is the
// request, its Detail and the result's as the local ones (core.detailTypes). Variant plans
// the change anew; an apply needs the plan's hash (Apply without Expect plans it first).
func (c *Client) Changes() core.Changes { return changes{c} }

type changes struct{ c *Client }

var _ core.Changes = changes{}

// changeAnswer is what every change endpoint answers.
type changeAnswer = struct {
	Body         []byte
	HTTPResponse *http.Response
	JSON200      *apiv1.Change
	JSONDefault  *apiv1.Error
}

func sent[R ~changeAnswer](r *R, err error) (apiv1.ChangeResponse, error) {
	if err != nil {
		return apiv1.ChangeResponse{}, fmt.Errorf("mangal serve: %w", err)
	}
	a := changeAnswer(*r)
	return answer(a.HTTPResponse.StatusCode, a.Body, a.JSON200, a.JSONDefault)
}

func opt[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}

func optList[T any](v []T) *[]T {
	if len(v) == 0 {
		return nil
	}
	return &v
}

// send plans the change, or applies it with f.
func (r changes) send(ctx context.Context, ch core.Change, apply bool, f apiv1.ApplyFields) (apiv1.ChangeResponse, error) {
	api := r.c.API
	p := &apply
	var resp apiv1.ChangeResponse
	var err error
	switch ch := ch.(type) {
	case core.MergeReq:
		resp, err = sent(api.MergeLibraryMangasWithResponse(ctx, &apiv1.MergeLibraryMangasParams{Apply: p},
			apiv1.MergeRequest{Keep: ch.Keep, Other: ch.Other, Expect: f.Expect, Force: f.Force, Confirm: f.Confirm}))
	case core.LinkReq:
		links := map[string]string{}
		for code, id := range ch.Links {
			links[string(code)] = id
		}
		resp, err = sent(api.LinkLibraryMangaWithResponse(ctx, &apiv1.LinkLibraryMangaParams{Apply: p},
			apiv1.LinkRequest{Manga: ch.Manga, Links: links, Rename: opt(ch.Rename), Watches: opt(ch.Watches), Expect: f.Expect, Force: f.Force, Confirm: f.Confirm}))
	case core.DismissPickReq:
		resp, err = sent(api.DismissLinkPickWithResponse(ctx, &apiv1.DismissLinkPickParams{Apply: p},
			apiv1.DismissPickRequest{Manga: ch.Manga, Source: string(ch.Source), Expect: f.Expect, Force: f.Force, Confirm: f.Confirm}))
	case core.BackupsReq:
		resp, err = sent(api.FillBackupLinksWithResponse(ctx, &apiv1.FillBackupLinksParams{Apply: p},
			apiv1.BackupsRequest{Mangas: optList(ch.Mangas), Replace: optList(ch.Replace), ReplaceAll: opt(ch.ReplaceAll), Expect: f.Expect, Force: f.Force, Confirm: f.Confirm}))
	case core.MarkReq:
		resp, err = sent(api.MarkLibraryMangasWithResponse(ctx, &apiv1.MarkLibraryMangasParams{Apply: p},
			apiv1.MarkRequest{Mangas: optList(ch.Mangas), Expect: f.Expect, Force: f.Force, Confirm: f.Confirm}))
	case core.RenameReq:
		resp, err = sent(api.RenameLibraryWithResponse(ctx, &apiv1.RenameLibraryParams{Apply: p},
			apiv1.RenameRequest{Refresh: opt(ch.Refresh), Chapters: opt(ch.Chapters), Fallback: opt(ch.Fallback), Cached: opt(ch.Cached),
				Paths: optList(ch.Paths), Only: optList(ch.Only), Expect: f.Expect, Force: f.Force, Confirm: f.Confirm}))
	case core.ForgetReq:
		resp, err = sent(api.ForgetLibraryMangasWithResponse(ctx, &apiv1.ForgetLibraryMangasParams{Apply: p},
			apiv1.ForgetRequest{Mangas: ch.Mangas, Expect: f.Expect, Force: f.Force, Confirm: f.Confirm}))
	case core.DeleteReq:
		resp, err = sent(api.DeleteLibraryMangaWithResponse(ctx, &apiv1.DeleteLibraryMangaParams{Apply: p},
			apiv1.DeleteRequest{Manga: ch.Manga, Chapter: opt(ch.Chapter), Expect: f.Expect, Force: f.Force, Confirm: f.Confirm}))
	case core.EmptyTrashReq:
		resp, err = sent(api.EmptyTrashWithResponse(ctx, &apiv1.EmptyTrashParams{Apply: p}, f))
	case core.MigrateReq:
		resp, err = sent(api.MigrateLibraryWithResponse(ctx, &apiv1.MigrateLibraryParams{Apply: p}, f))
	case core.UndoReq:
		id := ch.ID
		if id == 0 {
			if id, err = r.c.lastOp(ctx); err != nil {
				return resp, err
			}
		}
		resp, err = sent(api.UndoOpWithResponse(ctx, id, &apiv1.UndoOpParams{Apply: p}, f))
	case core.HealthFixReq:
		resp, err = sent(api.FixHealthFindingWithResponse(ctx, &apiv1.FixHealthFindingParams{Apply: p},
			apiv1.HealthFixRequest{Key: ch.Finding.Key, Choice: opt(ch.Choice), Expect: f.Expect, Force: f.Force, Confirm: f.Confirm}))
	case core.HistoryClearReq:
		resp, err = sent(api.ClearHistoryWithResponse(ctx, &apiv1.ClearHistoryParams{Apply: p},
			apiv1.HistoryClearRequest{Provider: opt(ch.Provider), MangaId: opt(ch.MangaID), Expect: f.Expect, Force: f.Force, Confirm: f.Confirm}))
	case core.BindReq:
		resp, err = sent(api.SetBindingWithResponse(ctx, &apiv1.SetBindingParams{Apply: p},
			apiv1.BindRequest{Provider: ch.Provider, MangaId: ch.MangaID, Source: string(ch.Code), Id: ch.ID, Expect: f.Expect, Force: f.Force, Confirm: f.Confirm}))
	case core.UnbindReq:
		resp, err = sent(api.RemoveBindingWithResponse(ctx, &apiv1.RemoveBindingParams{Apply: p, Provider: ch.Provider, MangaId: ch.MangaID,
			Source: string(ch.Code), Expect: f.Expect}))
	case nil:
		return resp, fmt.Errorf("no change: %w", core.ErrInvalid)
	default:
		return resp, fmt.Errorf("a %s isn't done through a remote server yet, run it without --remote on the server: %w", ch.Kind(), core.ErrInvalid)
	}
	var pc *core.PlanChangedError
	if errors.As(err, &pc) {
		pc.Plan.Change = ch
	}
	if resp.Plan != nil {
		resp.Plan.Change = ch
	}
	return resp, err
}

// lastOp is the newest operation not undone (what an undo without id undoes): the API
// takes an id.
func (c *Client) lastOp(ctx context.Context) (int64, error) {
	ops, err := c.Ops(ctx, 0)
	if err != nil {
		return 0, err
	}
	for _, op := range ops {
		if op.Undone.IsZero() {
			return op.ID, nil
		}
	}
	return 0, library.ErrNothingToUndo
}

func (r changes) Plan(ctx context.Context, ch core.Change) (core.Plan, error) {
	resp, err := r.send(ctx, ch, false, apiv1.ApplyFields{})
	if err != nil {
		return core.Plan{}, err
	}
	if resp.Plan == nil {
		return core.Plan{}, fmt.Errorf("mangal serve: no plan in the answer")
	}
	return *resp.Plan, nil
}

func (r changes) Apply(ctx context.Context, ch core.Change, opts core.ApplyOpts) (core.OpResult, error) {
	if opts.Expect == "" {
		plan, err := r.Plan(ctx, ch)
		if err != nil {
			return core.OpResult{}, err
		}
		opts.Expect = plan.Hash
	}
	// the API's confirm guards its own callers: mangal's CLI and TUI confirmed the plan
	confirm := string(core.KindEmptyTrash)
	if opts.Force {
		confirm = "force-undo"
	}
	f := apiv1.ApplyFields{Expect: &opts.Expect, Force: opt(opts.Force), Confirm: &confirm}
	waited := false
	for {
		resp, err := r.send(ctx, ch, true, f)
		if errors.Is(err, core.ErrBusy) && !opts.NoWait {
			if !waited && opts.OnWait != nil {
				// shortcut: the API doesn't say who holds the lock, the message says "another change"
				opts.OnWait(client.LibraryBusy{Holder: storage.LockHolder{PID: os.Getpid()}})
			}
			waited = true
			select {
			case <-ctx.Done():
				return core.OpResult{}, ctx.Err()
			case <-time.After(busyRetry):
			}
			continue
		}
		if err != nil {
			return core.OpResult{}, err
		}
		if resp.Result == nil {
			return core.OpResult{}, fmt.Errorf("mangal serve: no result in the answer")
		}
		if opts.OnLine != nil {
			for _, l := range resp.Result.Lines {
				opts.OnLine(l)
			}
		}
		return *resp.Result, nil
	}
}

func (r changes) ApplyPlan(ctx context.Context, p core.Plan, opts core.ApplyOpts) (core.OpResult, error) {
	if p.Change == nil {
		return core.OpResult{}, fmt.Errorf("a plan without its change: %w", core.ErrInvalid)
	}
	if opts.Expect == "" {
		opts.Expect = p.Hash
	}
	return r.Apply(ctx, p.Change, opts)
}

func (r changes) Variant(ctx context.Context, _ core.Plan, ch core.Change) (core.Plan, error) {
	return r.Plan(ctx, ch)
}

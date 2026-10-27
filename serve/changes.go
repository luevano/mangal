package serve

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/luevano/libmangal"
	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/selector"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// What `confirm` has to say to apply an irreversible change, or to force an undo.
const (
	ConfirmEmptyTrash = "empty-trash"
	ConfirmForceUndo  = "force-undo"
)

// applyFields are the plan/apply fields of a change's body.
type applyFields struct {
	expect  *string
	force   *bool
	confirm *string
}

// change plans the change, or with apply applies it: expect (the hash of the plan
// confirmed) is needed, a plan that hashes otherwise now is a 409 plan_changed with the
// new plan; an irreversible plan or a forced undo needs confirm (428).
func (a *API) change(ctx context.Context, ch core.Change, apply bool, f applyFields) (apiv1.ChangeResponse, error) {
	c, err := a.services(ctx)
	if err != nil {
		return apiv1.ChangeResponse{}, err
	}
	if !apply {
		plan, err := c.Changes.Plan(ctx, ch)
		if err != nil {
			return apiv1.ChangeResponse{}, err
		}
		return apiv1.ChangeResponse{Applied: false, Plan: &plan}, nil
	}
	expect, force, confirm := deref(f.expect), deref(f.force), deref(f.confirm)
	if expect == "" {
		return apiv1.ChangeResponse{}, badRequest("expect: apply needs the hash of the plan confirmed (POST without apply plans it)")
	}
	if force && confirm != ConfirmForceUndo {
		return apiv1.ChangeResponse{}, &apiError{status: http.StatusPreconditionRequired, code: CodeConfirm, confirm: ConfirmForceUndo,
			err: fmt.Errorf(`force loses what the plan's blocked reasons say: it needs "confirm": %q`, ConfirmForceUndo)}
	}
	if mayBeIrreversible(ch) {
		plan, err := c.Changes.Plan(ctx, ch)
		if err != nil {
			return apiv1.ChangeResponse{}, err
		}
		if plan.Irreversible && confirm != ConfirmEmptyTrash {
			return apiv1.ChangeResponse{}, &apiError{status: http.StatusPreconditionRequired, code: CodeConfirm, confirm: ConfirmEmptyTrash, plan: &plan,
				err: fmt.Errorf(`this can't be undone: it needs "confirm": %q`, ConfirmEmptyTrash)}
		}
	}
	res, err := c.Changes.Apply(ctx, ch, core.ApplyOpts{Expect: expect, Force: force, NoWait: true})
	if err != nil {
		return apiv1.ChangeResponse{}, err
	}
	resp := apiv1.ChangeResponse{Applied: true, Result: &res}
	if res.OpID != 0 {
		undo := fmt.Sprintf("POST /api/v1/ops/%d/undo (mangal library undo %d)", res.OpID, res.OpID)
		resp.Undo = &undo
	}
	a.Log.Infof("%s applied through the API: %s", res.Kind, res.Summary)
	a.publishOp(res)
	return resp, nil
}

// mayBeIrreversible are the changes whose plan can be irreversible.
func mayBeIrreversible(ch core.Change) bool {
	switch ch.Kind() {
	case core.KindEmptyTrash, core.KindHealthFix:
		return true
	}
	return false
}

func (a *API) publishOp(res core.OpResult) {
	if a.Events == nil {
		return
	}
	a.Events.Publish(TopicOp, "done", res.Summary, res)
}

func changeResponse(resp apiv1.ChangeResponse, err error) (apiv1.ChangeJSONResponse, error) {
	return apiv1.ChangeJSONResponse(resp), err
}

// MergeLibraryMangas implements apiv1.StrictServerInterface.
func (a *API) MergeLibraryMangas(ctx context.Context, request apiv1.MergeLibraryMangasRequestObject) (apiv1.MergeLibraryMangasResponseObject, error) {
	b := deref(request.Body)
	resp, err := changeResponse(a.change(ctx, core.MergeReq{Keep: b.Keep, Other: b.Other}, deref(request.Params.Apply), applyFields{b.Expect, b.Force, b.Confirm}))
	if err != nil {
		return nil, err
	}
	return apiv1.MergeLibraryMangas200JSONResponse{ChangeJSONResponse: resp}, nil
}

// MoveLibraryManga implements apiv1.StrictServerInterface: the provider's manga is found
// by its id (or a search), as `mangal library move` does.
func (a *API) MoveLibraryManga(ctx context.Context, request apiv1.MoveLibraryMangaRequestObject) (apiv1.MoveLibraryMangaResponseObject, error) {
	b := deref(request.Body)
	if deref(b.MangaId) == "" && deref(b.Query) == "" {
		return nil, badRequest("manga_id or query is needed")
	}
	loader, err := a.loader(b.Provider)
	if err != nil {
		return nil, err
	}
	cl, release, err := client.Hold(ctx, loader)
	if err != nil {
		return nil, err
	}
	defer release()
	to, err := selector.Find(ctx, client.Searcher(cl), deref(b.MangaId), deref(b.Query), deref(b.Selector))
	if err != nil {
		return nil, unprocessable(err)
	}
	resp, err := changeResponse(a.change(ctx, core.MoveReq{Manga: b.Manga, Provider: cl.Info(), To: to, Full: deref(b.Full)},
		deref(request.Params.Apply), applyFields{b.Expect, b.Force, b.Confirm}))
	if err != nil {
		return nil, err
	}
	return apiv1.MoveLibraryManga200JSONResponse{ChangeJSONResponse: resp}, nil
}

// RetitleLibraryManga implements apiv1.StrictServerInterface: the titles are found as
// `mangal library retitle` finds them.
func (a *API) RetitleLibraryManga(ctx context.Context, request apiv1.RetitleLibraryMangaRequestObject) (apiv1.RetitleLibraryMangaResponseObject, error) {
	b := deref(request.Body)
	if p := deref(b.Provider); p != "" {
		if _, err := a.loader(p); err != nil {
			return nil, err
		}
	}
	c, err := a.services(ctx)
	if err != nil {
		return nil, err
	}
	req, err := c.RetitleSource(ctx, request.Id, core.RetitleFrom{Provider: deref(b.Provider), MangaID: deref(b.MangaId), Query: deref(b.Query), Selector: deref(b.Selector)})
	if err != nil {
		if !errors.Is(err, core.ErrNotFound) && !errors.Is(err, core.ErrInvalid) {
			err = unprocessable(err)
		}
		return nil, err
	}
	resp, err := changeResponse(a.change(ctx, req, deref(request.Params.Apply), applyFields{b.Expect, b.Force, b.Confirm}))
	if err != nil {
		return nil, err
	}
	return apiv1.RetitleLibraryManga200JSONResponse{ChangeJSONResponse: resp}, nil
}

// LinkLibraryManga implements apiv1.StrictServerInterface.
func (a *API) LinkLibraryManga(ctx context.Context, request apiv1.LinkLibraryMangaRequestObject) (apiv1.LinkLibraryMangaResponseObject, error) {
	b := deref(request.Body)
	links := library.Links{}
	for code, id := range b.Links {
		links[lmmeta.IDCode(code)] = id
	}
	resp, err := changeResponse(a.change(ctx, core.LinkReq{Manga: b.Manga, Links: links, Rename: deref(b.Rename), Watches: deref(b.Watches)},
		deref(request.Params.Apply), applyFields{b.Expect, b.Force, b.Confirm}))
	if err != nil {
		return nil, err
	}
	return apiv1.LinkLibraryManga200JSONResponse{ChangeJSONResponse: resp}, nil
}

// FillBackupLinks implements apiv1.StrictServerInterface.
func (a *API) FillBackupLinks(ctx context.Context, request apiv1.FillBackupLinksRequestObject) (apiv1.FillBackupLinksResponseObject, error) {
	b := deref(request.Body)
	resp, err := changeResponse(a.change(ctx, core.BackupsReq{Mangas: deref(b.Mangas), Replace: deref(b.Replace), ReplaceAll: deref(b.ReplaceAll)},
		deref(request.Params.Apply), applyFields{b.Expect, b.Force, b.Confirm}))
	if err != nil {
		return nil, err
	}
	return apiv1.FillBackupLinks200JSONResponse{ChangeJSONResponse: resp}, nil
}

// MarkLibraryMangas implements apiv1.StrictServerInterface.
func (a *API) MarkLibraryMangas(ctx context.Context, request apiv1.MarkLibraryMangasRequestObject) (apiv1.MarkLibraryMangasResponseObject, error) {
	b := deref(request.Body)
	resp, err := changeResponse(a.change(ctx, core.MarkReq{Mangas: deref(b.Mangas)}, deref(request.Params.Apply), applyFields{b.Expect, b.Force, b.Confirm}))
	if err != nil {
		return nil, err
	}
	return apiv1.MarkLibraryMangas200JSONResponse{ChangeJSONResponse: resp}, nil
}

// RenameLibrary implements apiv1.StrictServerInterface.
func (a *API) RenameLibrary(ctx context.Context, request apiv1.RenameLibraryRequestObject) (apiv1.RenameLibraryResponseObject, error) {
	b := deref(request.Body)
	req := core.RenameReq{Refresh: deref(b.Refresh), Chapters: deref(b.Chapters), Fallback: deref(b.Fallback), Cached: deref(b.Cached),
		Paths: deref(b.Paths), Only: deref(b.Only)}
	resp, err := changeResponse(a.change(ctx, req, deref(request.Params.Apply), applyFields{b.Expect, b.Force, b.Confirm}))
	if err != nil {
		return nil, err
	}
	return apiv1.RenameLibrary200JSONResponse{ChangeJSONResponse: resp}, nil
}

// ForgetLibraryMangas implements apiv1.StrictServerInterface.
func (a *API) ForgetLibraryMangas(ctx context.Context, request apiv1.ForgetLibraryMangasRequestObject) (apiv1.ForgetLibraryMangasResponseObject, error) {
	b := deref(request.Body)
	resp, err := changeResponse(a.change(ctx, core.ForgetReq{Mangas: b.Mangas}, deref(request.Params.Apply), applyFields{b.Expect, b.Force, b.Confirm}))
	if err != nil {
		return nil, err
	}
	return apiv1.ForgetLibraryMangas200JSONResponse{ChangeJSONResponse: resp}, nil
}

// DeleteLibraryManga implements apiv1.StrictServerInterface.
func (a *API) DeleteLibraryManga(ctx context.Context, request apiv1.DeleteLibraryMangaRequestObject) (apiv1.DeleteLibraryMangaResponseObject, error) {
	b := deref(request.Body)
	resp, err := changeResponse(a.change(ctx, core.DeleteReq{Manga: b.Manga, Chapter: deref(b.Chapter)}, deref(request.Params.Apply), applyFields{b.Expect, b.Force, b.Confirm}))
	if err != nil {
		return nil, err
	}
	return apiv1.DeleteLibraryManga200JSONResponse{ChangeJSONResponse: resp}, nil
}

// EmptyTrash implements apiv1.StrictServerInterface.
func (a *API) EmptyTrash(ctx context.Context, request apiv1.EmptyTrashRequestObject) (apiv1.EmptyTrashResponseObject, error) {
	b := deref(request.Body)
	resp, err := changeResponse(a.change(ctx, core.EmptyTrashReq{}, deref(request.Params.Apply), applyFields{b.Expect, b.Force, b.Confirm}))
	if err != nil {
		return nil, err
	}
	return apiv1.EmptyTrash200JSONResponse{ChangeJSONResponse: resp}, nil
}

// MigrateLibrary implements apiv1.StrictServerInterface.
func (a *API) MigrateLibrary(ctx context.Context, request apiv1.MigrateLibraryRequestObject) (apiv1.MigrateLibraryResponseObject, error) {
	b := deref(request.Body)
	resp, err := changeResponse(a.change(ctx, core.MigrateReq{}, deref(request.Params.Apply), applyFields{b.Expect, b.Force, b.Confirm}))
	if err != nil {
		return nil, err
	}
	return apiv1.MigrateLibrary200JSONResponse{ChangeJSONResponse: resp}, nil
}

// UndoOp implements apiv1.StrictServerInterface.
func (a *API) UndoOp(ctx context.Context, request apiv1.UndoOpRequestObject) (apiv1.UndoOpResponseObject, error) {
	if err := a.canUndo(ctx, request.Id); err != nil {
		return nil, err
	}
	b := deref(request.Body)
	resp, err := changeResponse(a.change(ctx, core.UndoReq{ID: request.Id}, deref(request.Params.Apply), applyFields{b.Expect, b.Force, b.Confirm}))
	if err != nil {
		return nil, err
	}
	return apiv1.UndoOp200JSONResponse{ChangeJSONResponse: resp}, nil
}

// SetBinding implements apiv1.StrictServerInterface.
func (a *API) SetBinding(ctx context.Context, request apiv1.SetBindingRequestObject) (apiv1.SetBindingResponseObject, error) {
	b := deref(request.Body)
	req := core.BindReq{Provider: b.Provider, MangaID: b.MangaId, Code: lmmeta.IDCode(b.Source), ID: b.Id}
	resp, err := changeResponse(a.change(ctx, req, deref(request.Params.Apply), applyFields{b.Expect, b.Force, b.Confirm}))
	if err != nil {
		return nil, err
	}
	return apiv1.SetBinding200JSONResponse{ChangeJSONResponse: resp}, nil
}

// RemoveBinding implements apiv1.StrictServerInterface.
func (a *API) RemoveBinding(ctx context.Context, request apiv1.RemoveBindingRequestObject) (apiv1.RemoveBindingResponseObject, error) {
	p := request.Params
	req := core.UnbindReq{Provider: p.Provider, MangaID: p.MangaId, Code: lmmeta.IDCode(p.Source)}
	resp, err := changeResponse(a.change(ctx, req, deref(p.Apply), applyFields{expect: p.Expect}))
	if err != nil {
		return nil, err
	}
	return apiv1.RemoveBinding200JSONResponse{ChangeJSONResponse: resp}, nil
}

// ClearHistory implements apiv1.StrictServerInterface.
func (a *API) ClearHistory(ctx context.Context, request apiv1.ClearHistoryRequestObject) (apiv1.ClearHistoryResponseObject, error) {
	b := deref(request.Body)
	req := core.HistoryClearReq{Provider: deref(b.Provider), MangaID: deref(b.MangaId)}
	resp, err := changeResponse(a.change(ctx, req, deref(request.Params.Apply), applyFields{b.Expect, b.Force, b.Confirm}))
	if err != nil {
		return nil, err
	}
	return apiv1.ClearHistory200JSONResponse{ChangeJSONResponse: resp}, nil
}

// loader is the installed provider with the id (or an old id of it).
func (a *API) loader(id string) (libmangal.ProviderLoader, error) {
	loaders, err := a.loaders()
	if err != nil {
		return nil, err
	}
	loader, _, ok := libmangal.FindLoader(loaders, id)
	if !ok {
		return nil, fmt.Errorf("provider %q: %w", id, core.ErrNotFound)
	}
	return loader, nil
}

// loaders are the installed providers: the browser's, else the core's.
func (a *API) loaders() ([]libmangal.ProviderLoader, error) {
	if a.Browser != nil {
		return a.Browser.Loaders(), nil
	}
	c, err := a.services(context.Background())
	if err != nil {
		return nil, err
	}
	return c.Env().Loaders()
}

// manageKinds are the operations manage may undo: one manga at a time. The rest (rename,
// migrate, empty trash, clear history, settings, ...) need admin.
var manageKinds = []core.Kind{core.KindMerge, core.KindMove, core.KindLink, core.KindBackups, core.KindMark,
	core.KindForget, core.KindDelete, core.KindRetitle, core.KindHealthFix, core.KindBind, core.KindUnbind,
	core.KindDismissPick, "scan"}

// canUndo refuses undoing an operation that isn't manage's to whoever isn't an admin.
func (a *API) canUndo(ctx context.Context, id int64) error {
	if who, ok := identityOf(ctx); !ok || who.admin() {
		return nil
	}
	c, err := a.services(ctx)
	if err != nil {
		return err
	}
	ops, err := c.Library.Ops(ctx, 0)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(ops, func(op storage.Op) bool { return op.ID == id })
	if i >= 0 && !slices.Contains(manageKinds, core.Kind(ops[i].Kind)) {
		return newAPIError(http.StatusForbidden, CodeForbidden, "undoing a %s needs the admin permission", ops[i].Kind)
	}
	return nil
}

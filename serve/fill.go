package serve

import (
	"context"
	"errors"

	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// FillProviderManga implements apiv1.StrictServerInterface: planned as `mangal library
// fill` plans it, applied it's a download job of the other provider.
func (a *API) FillProviderManga(ctx context.Context, request apiv1.FillProviderMangaRequestObject) (apiv1.FillProviderMangaResponseObject, error) {
	b := deref(request.Body)
	for _, p := range []string{request.Provider, b.From} {
		if _, err := a.loader(p); err != nil {
			return nil, err
		}
	}
	c, err := a.services(ctx)
	if err != nil {
		return nil, err
	}
	plan, err := c.PlanFill(ctx, core.FillReq{
		Provider: request.Provider, MangaID: deref(b.MangaId), Query: deref(b.Query), Chapters: deref(b.Chapters),
		From: core.RetitleFrom{Provider: b.From, MangaID: deref(b.FromMangaId), Query: deref(b.FromQuery), Selector: deref(b.Selector)},
	})
	if err != nil {
		if !errors.Is(err, core.ErrNotFound) && !errors.Is(err, core.ErrInvalid) {
			err = unprocessable(err)
		}
		return nil, err
	}
	resp := apiv1.FillPlan{
		Provider: plan.Provider.ID, MangaId: plan.Manga.ID, MangaTitle: plan.Manga.Title, Folder: plan.Folder, Lines: plan.Lines(),
		Found: toAPIFill(plan.Found), NotFound: toAPIFill(plan.NotFound), Downloaded: toAPIFill(plan.Downloaded),
	}
	if plan.From.ID != "" {
		resp.From, resp.FromMangaId, resp.FromMangaTitle = new(plan.From.ID), new(plan.FromManga.ID), new(plan.FromManga.Title)
	}
	if deref(request.Params.Apply) && !plan.Nothing() {
		m, err := a.jobs()
		if err != nil {
			return nil, err
		}
		job := m.Add(plan.Job("api"))
		a.Log.Infof("download %d (asked through the API): %s", job.ID, job.Title)
		resp.Job = new(toAPIJob(core.JobOf(job)))
	}
	return apiv1.FillProviderManga200JSONResponse(resp), nil
}

func toAPIFill(chapters []core.FillChapter) []apiv1.FillChapter {
	out := make([]apiv1.FillChapter, len(chapters))
	for i, ch := range chapters {
		out[i] = apiv1.FillChapter{Number: ch.Number, Title: ch.Title}
		if ch.URL != "" {
			out[i].FromTitle, out[i].Url = new(ch.FromTitle), new(ch.URL)
		}
	}
	return out
}

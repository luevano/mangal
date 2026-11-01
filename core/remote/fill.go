package remote

import (
	"context"
	"fmt"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/web/apiv1"
)

func fillChaptersOf(list []apiv1.FillChapter) []core.FillChapter {
	out := make([]core.FillChapter, len(list))
	for i, ch := range list {
		out[i] = core.FillChapter{Number: ch.Number, Title: ch.Title, FromTitle: deref(ch.FromTitle), URL: deref(ch.Url)}
	}
	return out
}

func (c *Client) fill(ctx context.Context, req core.FillReq, apply bool) (apiv1.FillPlan, error) {
	body := apiv1.FillRequest{MangaId: opt(req.MangaID), Query: opt(req.Query), From: req.From.Provider, FromMangaId: opt(req.From.MangaID),
		FromQuery: opt(req.From.Query), Selector: opt(req.From.Selector), Chapters: opt(req.Chapters)}
	r, err := c.API.FillProviderMangaWithResponse(ctx, req.Provider, &apiv1.FillProviderMangaParams{Apply: &apply}, body)
	if err != nil {
		return apiv1.FillPlan{}, fmt.Errorf("mangal serve: %w", err)
	}
	return answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
}

// PlanFill plans getting the manga's unavailable chapters from the other provider's manga
// on the server. The plan only says what would be done: ApplyFill does it.
func (c *Client) PlanFill(ctx context.Context, req core.FillReq) (core.FillPlan, error) {
	p, err := c.fill(ctx, req, false)
	if err != nil {
		return core.FillPlan{}, err
	}
	return core.FillPlan{Provider: libmangal.ProviderInfo{ID: p.Provider}, From: libmangal.ProviderInfo{ID: deref(p.From)},
		Manga:     mangadata.MangaInfo{ID: p.MangaId, Title: p.MangaTitle},
		FromManga: mangadata.MangaInfo{ID: deref(p.FromMangaId), Title: deref(p.FromMangaTitle)},
		Folder:    p.Folder, Found: fillChaptersOf(p.Found), NotFound: fillChaptersOf(p.NotFound), Downloaded: fillChaptersOf(p.Downloaded)}, nil
}

// ApplyFill plans the fill again and starts its download job on the server.
func (c *Client) ApplyFill(ctx context.Context, req core.FillReq, origin string) error {
	p, err := c.fill(ctx, req, true)
	if err != nil || p.Job == nil {
		return err
	}
	j := c.Jobs()
	j.mu.Lock()
	j.origins[p.Job.Id] = origin
	j.mu.Unlock()
	j.keep(jobOf(*p.Job), false)
	return nil
}

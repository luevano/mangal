package core

import (
	"cmp"
	"context"
	"fmt"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/progress"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/selector"
	"github.com/luevano/mangal/v5/library"
)

// KindRetitle is giving a library manga's chapters another provider's titles.
const KindRetitle Kind = "retitle"

// RetitleReq gives the downloaded chapters of the library manga the titles of the
// provider's manga From has for their numbers (Titles, library.ChapterTitles of its
// chapters; RetitleSource finds them).
type RetitleReq struct {
	Manga    int64                  `json:"manga"`
	Provider libmangal.ProviderInfo `json:"-"`
	From     mangadata.MangaInfo    `json:"-"`
	Titles   map[float32]string     `json:"-"`
}

func (RetitleReq) Kind() Kind { return KindRetitle }

// RetitleFrom is where to take the titles from: the provider's (the library manga's
// current source's when empty) manga with the id, else the one the selector picks from a
// search of the query; with neither, the current source's manga.
type RetitleFrom struct {
	Provider, MangaID, Query, Selector string
}

// RetitleSource is the request to retitle the library manga from: the manga found, its
// chapters all listed (unavailable and retired ones too) with a client of its own.
func (c *Core) RetitleSource(ctx context.Context, manga int64, from RetitleFrom) (RetitleReq, error) {
	m, err := c.manga(manga)
	if err != nil {
		return RetitleReq{}, err
	}
	if from.Provider == "" {
		if len(m.Sources) == 0 {
			return RetitleReq{}, invalid("library manga %d has no source: give a provider", m.ID)
		}
		from.Provider = m.Sources[0].Provider
	}
	if from.MangaID == "" && from.Query == "" {
		from.Query = m.Title
		if len(m.Sources) != 0 && client.CurrentProviderID(from.Provider) == client.CurrentProviderID(m.Sources[0].Provider) {
			src := m.Sources[0]
			from.MangaID, from.Query = src.MangaID, cmp.Or(src.Title, m.Title)
		}
	}
	report(ctx, library.Progress{Phase: "finding the manga on " + from.Provider})
	provider, src, chapters, err := c.env.AllChapters(ctx, from.Provider, from.MangaID, from.Query, from.Selector)
	if err != nil {
		return RetitleReq{}, err
	}
	return RetitleReq{Manga: m.ID, Provider: provider, From: src.Info(), Titles: library.ChapterTitles(chapters)}, nil
}

// allChapters finds the provider's manga (selector.Find) with a client of its own whose
// chapter lists keep every chapter (client.Unfiltered), and lists them.
func allChapters(ctx context.Context, provider, id, query, sel string) (libmangal.ProviderInfo, mangadata.Manga, []mangadata.Chapter, error) {
	cl, _, err := client.Unfiltered(ctx, provider)
	if err != nil {
		return libmangal.ProviderInfo{}, nil, nil, err
	}
	defer cl.Close()
	manga, err := selector.Find(ctx, client.Searcher(cl), id, query, sel)
	if err != nil {
		return cl.Info(), nil, nil, err
	}
	progress.Report(ctx, progress.Status{Source: cl.Info().ID, Message: fmt.Sprintf("listing the chapters of %q", manga.Info().Title)})
	chapters, err := client.MangaChapters(ctx, cl, manga)
	return cl.Info(), manga, chapters, err
}

// RetitleDetail is a retitle's plan.
type RetitleDetail struct {
	Provider libmangal.ProviderInfo
	From     mangadata.MangaInfo
	Plan     library.RetitlePlan
}

func (r RetitleReq) plan(ctx context.Context, c *Core) (*prepared, error) {
	if r.Provider.ID == "" {
		return nil, invalid("no manga to take the titles from")
	}
	m, err := c.manga(r.Manga)
	if err != nil {
		return nil, err
	}
	files, err := c.existingFiles()
	if err != nil {
		return nil, err
	}
	loaders, err := c.env.Loaders()
	if err != nil {
		return nil, err
	}
	rt := library.Retitler{Files: files, Loaders: loaders, ChapterName: c.env.ChapterName}
	rp, err := rt.PlanRetitle(m, r.Titles)
	if err != nil {
		return nil, err
	}
	todo := rp.ToDo()
	summary := fmt.Sprintf("retitle %d chapters of %d %q from %s %q", len(todo), m.ID, m.Title, r.Provider.ID, r.From.Title)
	return &prepared{
		plan: Plan{Summary: summary, Lines: rp.Lines(), Nothing: len(todo) == 0,
			Detail: RetitleDetail{Provider: r.Provider, From: r.From, Plan: rp}},
		tables: libraryTables,
		apply: func(ctx context.Context, opts ApplyOpts, res *OpResult) error {
			done := 0
			defer func() { res.Detail = done }()
			return c.journaled(ctx, "retitle", summary, res, func(j library.Journal) error {
				rt.Journal, rt.OnProgress = j, progressOf(ctx)
				return rt.ApplyRetitleContext(ctx, rp, func(i int, err error) {
					if err != nil {
						opts.line(res, fmt.Sprintf("skipped %s: %s", todo[i].Line(), err), true)
						return
					}
					done++
					opts.line(res, todo[i].Line(), false)
				})
			})
		},
	}, nil
}

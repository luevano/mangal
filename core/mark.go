package core

import (
	"cmp"
	"context"
	"fmt"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
)

// MarkReq writes the .libmangal.json of library mangas from what the library knows: of
// Mangas, replacing one that's broken or disagrees with the library ("library mark");
// without Mangas, of every folder that has none ("library scan --mark").
type MarkReq struct {
	Mangas []int64 `json:"mangas,omitempty"`
}

func (MarkReq) Kind() Kind { return KindMark }

// MarkDetail is what a mark would write.
type MarkDetail struct {
	// Marked are the folders to mark.
	Marked []library.Marked
	// Result is the dry run of every folder without a marker (no Mangas).
	Result library.MarkResult
	// Unchanged are the Mangas with nothing to do.
	Unchanged []storage.Manga
}

// MarkedLine is "<verb> <folder> as <provider> <manga id> (<evidence>)".
func MarkedLine(verb string, m library.Marked) string {
	as := cmp.Or(m.Info.Provider, "no provider")
	if m.Info.Manga.ID != "" {
		as += " " + m.Info.Manga.ID
	}
	return fmt.Sprintf("%s %s as %s (%s)", verb, m.Manga.Path(), as, m.Evidence)
}

func (c *Core) markOptions(dryRun bool) (library.MarkOptions, error) {
	loaders, err := c.env.Loaders()
	if err != nil {
		return library.MarkOptions{}, err
	}
	return library.MarkOptions{Loaders: loaders, ProviderDirs: c.env.ProviderDirs(), ProviderName: c.env.ProviderName, DryRun: dryRun}, nil
}

func (r MarkReq) plan(ctx context.Context, c *Core) (*prepared, error) {
	files, err := c.existingFiles()
	if err != nil {
		return nil, err
	}
	files.OnProgress = progressOf(ctx)
	opts, err := c.markOptions(true)
	if err != nil {
		return nil, err
	}
	var d MarkDetail
	var mangas []storage.Manga
	var lines []string
	if len(r.Mangas) == 0 {
		if d.Result, err = files.MarkContext(ctx, opts); err != nil {
			return nil, err
		}
		d.Marked = d.Result.Marked
		for _, m := range d.Marked {
			lines = append(lines, MarkedLine("would mark", m))
		}
	} else {
		for _, id := range r.Mangas {
			m, err := c.manga(id)
			if err != nil {
				return nil, err
			}
			marked, do, err := files.MarkManga(m, opts)
			if err != nil {
				return nil, err
			}
			if !do {
				d.Unchanged = append(d.Unchanged, m)
				lines = append(lines, fmt.Sprintf("%d %s: nothing to do (its %s agrees, or no folder, or nothing to go by)", m.ID, m.Title, libmangal.FilenameMangaDirInfo))
				continue
			}
			mangas = append(mangas, m)
			d.Marked = append(d.Marked, marked)
			lines = append(lines, MarkedLine("would mark", marked))
		}
	}
	summary := fmt.Sprintf("mark %d folders", len(d.Marked))
	return &prepared{
		plan:   Plan{Summary: summary, Lines: lines, Nothing: len(d.Marked) == 0, Detail: d},
		tables: libraryTables,
		apply: func(ctx context.Context, opts ApplyOpts, res *OpResult) error {
			mark, err := c.markOptions(false)
			if err != nil {
				return err
			}
			mark.OnMarked = func(m library.Marked) { opts.line(res, MarkedLine("marked", m), false) }
			return c.journaled(ctx, "mark", summary, res, func(j library.Journal) error {
				files.Journal, files.OnProgress = j, progressOf(ctx)
				if len(r.Mangas) == 0 {
					result, err := files.MarkContext(ctx, mark)
					res.Detail = result
					return err
				}
				var result library.MarkResult
				defer func() { res.Detail = result }()
				for i, m := range mangas {
					if err := ctx.Err(); err != nil {
						return err
					}
					report(ctx, library.Progress{Phase: "marking", Done: i + 1, Total: len(mangas), Item: m.Title})
					marked, done, err := files.MarkManga(m, mark)
					if err != nil {
						return err
					}
					if done {
						result.Marked = append(result.Marked, marked)
						mark.OnMarked(marked)
					}
				}
				return nil
			})
		},
	}, nil
}

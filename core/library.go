package core

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	stringutil "github.com/luevano/mangal/v5/util/string"
	"github.com/spf13/afero"
)

// Library reads the library.
type Library struct {
	core *Core
}

// Mangas are the library's mangas.
func (l *Library) Mangas() ([]storage.Manga, error) {
	return l.core.env.Storage.Library().Mangas()
}

// Manga is the library manga with the id, a *NotFoundError when there's none.
func (l *Library) Manga(id int64) (storage.Manga, error) {
	return l.core.manga(id)
}

// Downloads are the manga's downloaded chapters.
func (l *Library) Downloads(manga int64) ([]storage.Download, error) {
	return l.core.env.Storage.Library().Downloads(manga)
}

// Trash is what's in the trash, oldest first.
func (l *Library) Trash() ([]library.TrashBatch, error) {
	return library.Trash(l.core.env.FS, l.core.env.Root())
}

// Ops are the journal's operations, newest first (limit 0: all).
func (l *Library) Ops(limit int) ([]storage.Op, error) {
	return l.core.env.Storage.Journal().Ops(limit)
}

// History is the read chapters (of the provider, or its manga), last read first.
func (l *Library) History(provider, mangaID string) ([]storage.Read, error) {
	return l.core.env.Storage.History().List(provider, mangaID)
}

// Root is the download directory.
func (l *Library) Root() string { return l.core.env.Root() }

// manga is the library manga with the id, a *NotFoundError when there's none.
func (c *Core) manga(id int64) (storage.Manga, error) {
	m, ok, err := c.env.Storage.Library().MangaByID(id)
	if err != nil {
		return m, err
	}
	if !ok {
		return m, &NotFoundError{What: fmt.Sprintf("no manga %d in the library", id)}
	}
	return m, nil
}

// rel is p relative to the download directory when it's inside it.
func (c *Core) rel(p string) string {
	root := c.env.Root()
	if r, err := filepath.Rel(root, p); err == nil && isInsideRel(r) {
		return r
	}
	return p
}

// isInsideRel is true for a relative path that doesn't leave its directory.
func isInsideRel(rel string) bool {
	return rel != ".." && !filepath.IsAbs(rel) && (len(rel) < 3 || rel[:3] != ".."+string(filepath.Separator))
}

// MergeReq merges the library manga Other into Keep: Other's chapters Keep doesn't have
// move into Keep's folder, the ones both have go to the trash with what's left of Other's
// folder.
type MergeReq struct {
	Keep  int64 `json:"keep"`
	Other int64 `json:"other"`
}

func (MergeReq) Kind() Kind { return KindMerge }

func (r MergeReq) plan(ctx context.Context, c *Core) (*prepared, error) {
	keep, err := c.manga(r.Keep)
	if err != nil {
		return nil, err
	}
	other, err := c.manga(r.Other)
	if err != nil {
		return nil, err
	}
	files, err := c.existingFiles()
	if err != nil {
		return nil, err
	}
	files.OnProgress = progressOf(ctx)
	mp, err := files.PlanMerge(keep, other)
	if err != nil {
		return nil, err
	}
	lines := []string{fmt.Sprintf("keep %q (%s), merge %q (%s) into it:", mp.Keep.Title, mp.Keep.Path(), mp.Other.Title, mp.Other.Path())}
	for _, m := range mp.Moves {
		lines = append(lines, fmt.Sprintf("  move   %s", filepath.Base(m.From)))
	}
	for _, p := range mp.Trashed {
		lines = append(lines, fmt.Sprintf("  trash  %s (already in %q)", filepath.Base(p), mp.Keep.Title))
	}
	summary := fmt.Sprintf("merge %d %q into %d %q", mp.Other.ID, mp.Other.Title, mp.Keep.ID, mp.Keep.Title)
	return &prepared{
		plan:   Plan{Summary: summary, Lines: lines, Detail: mp},
		tables: libraryTables,
		apply: func(ctx context.Context, _ ApplyOpts, res *OpResult) error {
			return c.journaled(ctx, "merge", summary, res, func(j library.Journal) error {
				files.Journal, files.OnProgress = j, progressOf(ctx)
				return files.ApplyMergeContext(ctx, mp)
			})
		},
	}, nil
}

// ForgetReq forgets library mangas (their folders are gone, or they've no chapters), their
// files aren't touched. The journal calls it "remove".
type ForgetReq struct {
	Mangas []int64 `json:"mangas"`
}

func (ForgetReq) Kind() Kind { return KindForget }

func (r ForgetReq) plan(_ context.Context, c *Core) (*prepared, error) {
	if len(r.Mangas) == 0 {
		return nil, invalid("no mangas to forget")
	}
	var mangas []storage.Manga
	var lines []string
	for _, id := range r.Mangas {
		m, err := c.manga(id)
		if err != nil {
			return nil, err
		}
		mangas = append(mangas, m)
		lines = append(lines, fmt.Sprintf("forget library manga %d %s (%s), its files aren't touched", m.ID, m.Title, m.Path()))
	}
	return &prepared{
		plan:   Plan{Summary: forgetSummary(mangas), Lines: lines, Detail: mangas},
		tables: libraryTables,
		apply: func(ctx context.Context, _ ApplyOpts, res *OpResult) error {
			return c.forget(ctx, mangas, res)
		},
	}, nil
}

func forgetSummary(mangas []storage.Manga) string {
	if len(mangas) == 1 {
		return fmt.Sprintf("remove missing %d %q", mangas[0].ID, mangas[0].Title)
	}
	return fmt.Sprintf("remove %d missing mangas", len(mangas))
}

func (c *Core) forget(ctx context.Context, mangas []storage.Manga, res *OpResult) error {
	ids := make([]int64, len(mangas))
	for i, m := range mangas {
		ids[i] = m.ID
	}
	return c.journaled(ctx, "remove", forgetSummary(mangas), res, func(library.Journal) error {
		return c.env.Storage.Library().RemoveMangas(ids...)
	})
}

// DeleteReq deletes a library manga, or only one of its chapters (Chapter: the download's
// path, file name or chapter number): the files go to the trash, the rows go. A manga
// whose folder is gone is only forgotten.
type DeleteReq struct {
	Manga   int64  `json:"manga"`
	Chapter string `json:"chapter,omitempty"`
}

func (DeleteReq) Kind() Kind { return KindDelete }

// DeletePlan is what a delete does.
type DeletePlan struct {
	Manga storage.Manga
	// Download is the chapter deleted, nil for the whole manga.
	Download *storage.Download
	// Missing is a manga whose folder is gone: it's only forgotten.
	Missing bool
	// Chapters are the manga's.
	Chapters int
}

// ChapterTitle is the chapter's "number - title".
func ChapterTitle(dl storage.Download) string {
	title := stringutil.FormatFloat32(dl.Chapter)
	if dl.ChapterTitle != "" {
		title += " - " + dl.ChapterTitle
	}
	return title
}

func (r DeleteReq) plan(_ context.Context, c *Core) (*prepared, error) {
	m, err := c.manga(r.Manga)
	if err != nil {
		return nil, err
	}
	files, err := c.existingFiles()
	if err != nil {
		return nil, err
	}
	lib := c.env.Storage.Library()
	downloads, err := lib.Downloads(m.ID)
	if err != nil {
		return nil, err
	}
	dp := DeletePlan{Manga: m, Chapters: len(downloads)}
	if r.Chapter != "" {
		dl, err := findDownload(downloads, r.Chapter, m.Path(), files.Root)
		if err != nil {
			return nil, err
		}
		dp.Download = &dl
		title := ChapterTitle(dl)
		summary := fmt.Sprintf("delete chapter %s of %d %q", title, m.ID, m.Title)
		lines := []string{fmt.Sprintf("trash %s", c.rel(dl.Path)), fmt.Sprintf("forget chapter %s of %d %q", title, m.ID, m.Title)}
		return &prepared{
			plan:   Plan{Summary: summary, Lines: lines, Detail: dp},
			tables: libraryTables,
			apply: func(ctx context.Context, _ ApplyOpts, res *OpResult) error {
				return c.journaled(ctx, "delete", summary, res, func(j library.Journal) error {
					files.Journal = j
					return files.DeleteDownload(m, dl)
				})
			},
		}, nil
	}
	exists, err := afero.DirExists(c.env.FS, m.Path())
	if err != nil {
		return nil, err
	}
	if !exists {
		dp.Missing = true
		lines := []string{fmt.Sprintf("its folder %s is gone: forget library manga %d %s", m.Path(), m.ID, m.Title)}
		return &prepared{
			plan:   Plan{Summary: forgetSummary([]storage.Manga{m}), Lines: lines, Detail: dp},
			tables: libraryTables,
			apply: func(ctx context.Context, _ ApplyOpts, res *OpResult) error {
				return c.forget(ctx, []storage.Manga{m}, res)
			},
		}, nil
	}
	var lines []string
	if m.Dir == "" {
		for _, dl := range downloads {
			lines = append(lines, "trash "+c.rel(dl.Path))
		}
	} else {
		lines = append(lines, fmt.Sprintf("trash %s (%s)", c.rel(m.Path()), stringutil.Quantify(len(downloads), "chapter", "chapters")))
	}
	lines = append(lines, fmt.Sprintf("forget library manga %d %q", m.ID, m.Title))
	summary := fmt.Sprintf("delete %d %q", m.ID, m.Title)
	return &prepared{
		plan:   Plan{Summary: summary, Lines: lines, Detail: dp},
		tables: libraryTables,
		apply: func(ctx context.Context, _ ApplyOpts, res *OpResult) error {
			return c.journaled(ctx, "delete", summary, res, func(j library.Journal) error {
				files.Journal = j
				return files.DeleteManga(m)
			})
		},
	}, nil
}

// findDownload is the download whose path (absolute, or relative to the manga's folder
// or the download directory), file name or chapter number is chapter.
func findDownload(downloads []storage.Download, chapter, dir, root string) (storage.Download, error) {
	var found []storage.Download
	for _, dl := range downloads {
		if dl.Path == chapter || filepath.Join(dir, chapter) == dl.Path || filepath.Join(root, chapter) == dl.Path {
			return dl, nil
		}
		if filepath.Base(dl.Path) == chapter || stringutil.FormatFloat32(dl.Chapter) == chapter {
			found = append(found, dl)
		}
	}
	switch len(found) {
	case 0:
		return storage.Download{}, &NotFoundError{What: fmt.Sprintf("no chapter %q downloaded", chapter)}
	case 1:
		return found[0], nil
	}
	return storage.Download{}, invalid("chapter %s is %d files, give its path in the manga's folder", chapter, len(found))
}

// EmptyTrashReq deletes what's in the trash for good: it can't be undone, nor can the
// operations that put files there after it.
type EmptyTrashReq struct{}

func (EmptyTrashReq) Kind() Kind { return KindEmptyTrash }

// EmptyTrashPlan is what's in the trash and the operations whose undo needs it.
type EmptyTrashPlan struct {
	Batches []library.TrashBatch
	Files   int
	Size    int64
	Blocked []storage.Op
}

// BlockedLines are "these operations can't be undone after it:" and the operations, none
// when there are none.
func (p EmptyTrashPlan) BlockedLines() []string {
	if len(p.Blocked) == 0 {
		return nil
	}
	lines := []string{"these operations can't be undone after it:"}
	for _, op := range p.Blocked {
		lines = append(lines, fmt.Sprintf("  %d  %s  %s  %s", op.ID, op.Started.Format(time.DateTime), op.Kind, op.Summary))
	}
	return lines
}

func (EmptyTrashReq) plan(_ context.Context, c *Core) (*prepared, error) {
	root := c.env.Root()
	batches, err := library.Trash(c.env.FS, root)
	if err != nil {
		return nil, err
	}
	tp := EmptyTrashPlan{Batches: batches}
	for _, b := range batches {
		tp.Files += b.Files
		tp.Size += b.Size
	}
	if len(batches) == 0 {
		return &prepared{plan: Plan{Lines: []string{"the trash is empty"}, Nothing: true, Detail: tp}, tables: journalTables}, nil
	}
	if tp.Blocked, err = c.undoer().NeedTrash(root); err != nil {
		return nil, err
	}
	lines := append(tp.BlockedLines(), fmt.Sprintf("delete %s (%s) in the trash for good", stringutil.Quantify(tp.Files, "file", "files"), stringutil.HumanSize(tp.Size)))
	return &prepared{
		plan:   Plan{Summary: "empty the trash", Lines: lines, Irreversible: true, Detail: tp},
		tables: journalTables,
		apply: func(_ context.Context, _ ApplyOpts, res *OpResult) error {
			snapshot, err := c.ops.Snapshot("empty-trash")
			if err != nil {
				return err
			}
			res.Snapshot = snapshot
			return library.EmptyTrash(c.env.FS, root)
		},
	}, nil
}

// HistoryClearReq forgets the chapters read: all, the provider's or its manga's.
type HistoryClearReq struct {
	Provider string `json:"provider,omitempty"`
	MangaID  string `json:"manga_id,omitempty"`
}

func (HistoryClearReq) Kind() Kind { return KindHistoryClear }

func (r HistoryClearReq) plan(_ context.Context, c *Core) (*prepared, error) {
	if r.MangaID != "" && r.Provider == "" {
		return nil, invalid("a manga id needs its provider")
	}
	history := c.env.Storage.History()
	reads, err := history.List(r.Provider, r.MangaID)
	if err != nil {
		return nil, err
	}
	if len(reads) == 0 {
		return &prepared{plan: Plan{Lines: []string{"no history"}, Nothing: true}, tables: readTables}, nil
	}
	what := "every read chapter"
	switch {
	case r.MangaID != "":
		what = fmt.Sprintf("the read chapters of %s %s", r.Provider, r.MangaID)
	case r.Provider != "":
		what = "the read chapters of " + r.Provider
	}
	summary := "forget " + what
	lines := []string{fmt.Sprintf("forget %s", stringutil.Quantify(len(reads), "read chapter", "read chapters"))}
	return &prepared{
		plan:   Plan{Summary: summary, Lines: lines, Detail: reads},
		tables: readTables,
		apply: func(ctx context.Context, opts ApplyOpts, res *OpResult) error {
			var n int64
			err := c.journaled(ctx, "history-clear", summary, res, func(library.Journal) error {
				var err error
				n, err = history.Clear(r.Provider, r.MangaID)
				return err
			})
			if err == nil {
				res.Detail = n
				opts.line(res, "forgot "+stringutil.Quantify(int(n), "read chapter", "read chapters"), false)
			}
			return err
		},
	}, nil
}

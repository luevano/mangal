package serve

import (
	"context"
	"path/filepath"
	"slices"
	"strings"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/web/apiv1"
)

func toAPILibraryManga(m storage.Manga, watched bool) apiv1.LibraryManga {
	out := apiv1.LibraryManga{Id: m.ID, Title: m.Title, Path: m.Path(), Chapters: m.Chapters,
		MetadataIds: []apiv1.MetadataID{}, Sources: []apiv1.LibrarySource{}}
	if m.Dir != "" {
		out.Dir = &m.Dir
	}
	if m.Status != "" {
		out.Status = &m.Status
	}
	out.StatusUpdated = optTime(m.StatusUpdated)
	for _, id := range m.MetadataIDs {
		out.MetadataIds = append(out.MetadataIds, apiv1.MetadataID{Source: string(id.Code), Id: id.Raw})
	}
	for _, s := range m.Sources {
		out.Sources = append(out.Sources, apiv1.LibrarySource{Provider: s.Provider, MangaId: s.MangaID, Title: s.Title,
			Moved: optTime(s.Moved), Current: s.Current})
	}
	if watched {
		out.Watched = &watched
	}
	return out
}

// watchedMangas are the library mangas with a watch, and the watches by library manga.
func (a *API) watchedMangas(ctx context.Context) (map[int64][]watch.Watch, error) {
	list, err := a.Manager().List(ctx)
	if err != nil {
		return nil, err
	}
	by := map[int64][]watch.Watch{}
	for _, w := range list {
		if w.LibraryManga != 0 {
			by[w.LibraryManga] = append(by[w.LibraryManga], w)
		}
	}
	return by, nil
}

// page is the part of list limit and offset ask for.
func page[T any](list []T, limit, offset *int) []T {
	o := min(max(deref(offset), 0), len(list))
	list = list[o:]
	if l := deref(limit); l > 0 && l < len(list) {
		list = list[:l]
	}
	return list
}

// ListLibraryMangas implements apiv1.StrictServerInterface.
func (a *API) ListLibraryMangas(ctx context.Context, request apiv1.ListLibraryMangasRequestObject) (apiv1.ListLibraryMangasResponseObject, error) {
	c, err := a.services(ctx)
	if err != nil {
		return nil, err
	}
	ml, err := c.Library.Mangas(ctx, core.MangasOptions{})
	if err != nil {
		return nil, err
	}
	mangas := ml.Mangas
	watched, err := a.watchedMangas(ctx)
	if err != nil {
		return nil, err
	}
	p := request.Params
	duplicates := map[int64]bool{}
	if deref(p.Duplicates) {
		dups, err := c.Library.Duplicates(ctx)
		if err != nil {
			return nil, err
		}
		for _, d := range dups {
			for _, m := range d.Mangas {
				duplicates[m.Manga.ID] = true
			}
		}
	}
	q := strings.ToLower(deref(p.Q))
	var list []apiv1.LibraryManga
	for _, m := range mangas {
		_, isWatched := watched[m.ID]
		switch {
		case q != "" && !mangaMatches(m, q):
			continue
		case p.Status != nil && !statusMatches(m.Status, *p.Status):
			continue
		case deref(p.Provider) != "" && !hasSource(m, *p.Provider):
			continue
		case deref(p.Duplicates) && !duplicates[m.ID]:
			continue
		case p.Watched != nil && *p.Watched != isWatched:
			continue
		}
		list = append(list, toAPILibraryManga(m, isWatched))
	}
	return apiv1.ListLibraryMangas200JSONResponse{Total: len(list), Mangas: orEmpty(page(list, p.Limit, p.Offset))}, nil
}

func mangaMatches(m storage.Manga, q string) bool {
	if strings.Contains(strings.ToLower(m.Title), q) || strings.Contains(strings.ToLower(m.Dir), q) {
		return true
	}
	for _, s := range m.Sources {
		if strings.Contains(strings.ToLower(s.Title), q) {
			return true
		}
	}
	return false
}

func statusMatches(status, want string) bool {
	if want == "unknown" {
		return status == ""
	}
	return strings.EqualFold(status, want)
}

func hasSource(m storage.Manga, provider string) bool {
	for _, s := range m.Sources {
		if s.Provider == provider {
			return true
		}
	}
	return false
}

// GetLibraryManga implements apiv1.StrictServerInterface.
func (a *API) GetLibraryManga(ctx context.Context, request apiv1.GetLibraryMangaRequestObject) (apiv1.GetLibraryMangaResponseObject, error) {
	c, err := a.services(ctx)
	if err != nil {
		return nil, err
	}
	m, err := c.Library.Manga(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	watched, err := a.watchedMangas(ctx)
	if err != nil {
		return nil, err
	}
	bindings, err := c.Env().Storage.Bindings().OfLibraryManga(m.ID)
	if err != nil {
		return nil, err
	}
	fsys := c.Env().FS
	_, statErr := fsys.Stat(m.Path())
	_, markerErr := fsys.Stat(filepath.Join(m.Path(), libmangal.FilenameMangaDirInfo))
	return apiv1.GetLibraryManga200JSONResponse{
		Manga:    toAPILibraryManga(m, len(watched[m.ID]) != 0),
		Bindings: orEmpty(bindings),
		Watches:  orEmpty(watched[m.ID]),
		Marker:   markerErr == nil,
		Missing:  statErr != nil,
	}, nil
}

func toAPIDownload(d storage.Download) apiv1.Download {
	out := apiv1.Download{Path: d.Path, Provider: d.Provider, MangaId: d.MangaID, MangaTitle: d.MangaTitle,
		Volume: d.Volume, Chapter: d.Chapter, ChapterTitle: d.ChapterTitle, Format: d.Format, Pages: d.Pages, Downloaded: d.Downloaded}
	if d.ChapterURL != "" {
		out.ChapterUrl = &d.ChapterURL
	}
	return out
}

// ListLibraryChapters implements apiv1.StrictServerInterface.
func (a *API) ListLibraryChapters(ctx context.Context, request apiv1.ListLibraryChaptersRequestObject) (apiv1.ListLibraryChaptersResponseObject, error) {
	c, err := a.services(ctx)
	if err != nil {
		return nil, err
	}
	m, err := c.Library.Manga(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	downloads, err := c.Library.Downloads(ctx, m.ID)
	if err != nil {
		return nil, err
	}
	// the reads of its sources, by chapter number
	type readKey struct {
		provider, manga string
		chapter         float32
	}
	reads := map[readKey]storage.Read{}
	for _, s := range m.Sources {
		list, err := c.Library.History(ctx, s.Provider, s.MangaID)
		if err != nil {
			return nil, err
		}
		for _, r := range list {
			reads[readKey{r.Provider, r.MangaID, r.Chapter}] = r
		}
	}
	out := apiv1.ListLibraryChapters200JSONResponse{}
	for _, d := range downloads {
		ch := apiv1.LibraryChapter{Download: toAPIDownload(d)}
		if r, ok := reads[readKey{d.Provider, d.MangaID, d.Chapter}]; ok {
			ch.Reads, ch.LastRead = r.Count, optTime(r.Last)
		}
		out = append(out, ch)
	}
	return out, nil
}

// ListDownloads implements apiv1.StrictServerInterface: newest first.
func (a *API) ListDownloads(ctx context.Context, request apiv1.ListDownloadsRequestObject) (apiv1.ListDownloadsResponseObject, error) {
	c, err := a.services(ctx)
	if err != nil {
		return nil, err
	}
	p := request.Params
	downloads, err := c.Env().Storage.Library().ListDownloads(deref(p.Provider), deref(p.MangaId))
	if err != nil {
		return nil, err
	}
	list := make([]apiv1.Download, 0, len(downloads))
	for _, d := range downloads {
		list = append(list, toAPIDownload(d))
	}
	sortNewest(list)
	return apiv1.ListDownloads200JSONResponse{Total: len(list), Downloads: orEmpty(page(list, p.Limit, p.Offset))}, nil
}

func sortNewest(list []apiv1.Download) {
	// stable: the same time keeps the library's order (by manga and chapter)
	slices.SortStableFunc(list, func(x, y apiv1.Download) int { return y.Downloaded.Compare(x.Downloaded) })
}

// ListDuplicates implements apiv1.StrictServerInterface.
func (a *API) ListDuplicates(ctx context.Context, _ apiv1.ListDuplicatesRequestObject) (apiv1.ListDuplicatesResponseObject, error) {
	c, err := a.services(ctx)
	if err != nil {
		return nil, err
	}
	dups, err := c.Library.Duplicates(ctx)
	if err != nil {
		return nil, err
	}
	out := apiv1.ListDuplicates200JSONResponse{}
	for _, d := range dups {
		dup := apiv1.Duplicate{Source: string(d.ID), Id: d.Raw, Mangas: []apiv1.LibraryManga{}}
		for _, m := range d.Mangas {
			dup.Mangas = append(dup.Mangas, toAPILibraryManga(m.Manga, m.Watched))
		}
		out = append(out, dup)
	}
	return out, nil
}

// GetTrash implements apiv1.StrictServerInterface.
func (a *API) GetTrash(ctx context.Context, _ apiv1.GetTrashRequestObject) (apiv1.GetTrashResponseObject, error) {
	c, err := a.services(ctx)
	if err != nil {
		return nil, err
	}
	batches, err := c.Library.Trash(ctx)
	if err != nil {
		return nil, err
	}
	out := apiv1.GetTrash200JSONResponse{Batches: []apiv1.TrashBatch{}}
	for _, b := range batches {
		out.Batches = append(out.Batches, toAPITrash(b))
		out.Files += b.Files
		out.Size += b.Size
	}
	return out, nil
}

func toAPITrash(b library.TrashBatch) apiv1.TrashBatch {
	return apiv1.TrashBatch{Name: b.Name, Path: b.Path, Files: b.Files, Size: b.Size}
}

func toAPIOp(op storage.Op) apiv1.Op {
	out := apiv1.Op{Id: op.ID, Kind: op.Kind, Summary: op.Summary, Started: op.Started, Steps: op.Steps,
		Finished: optTime(op.Finished), Undone: optTime(op.Undone)}
	if op.Error != "" {
		out.Error = &op.Error
	}
	if op.Snapshot != "" {
		out.Snapshot = &op.Snapshot
	}
	if op.Interfered != "" {
		out.Interfered = &op.Interfered
	}
	return out
}

// ListOps implements apiv1.StrictServerInterface.
func (a *API) ListOps(ctx context.Context, request apiv1.ListOpsRequestObject) (apiv1.ListOpsResponseObject, error) {
	c, err := a.services(ctx)
	if err != nil {
		return nil, err
	}
	ops, err := c.Library.Ops(ctx, deref(request.Params.Limit))
	if err != nil {
		return nil, err
	}
	out := apiv1.ListOps200JSONResponse{}
	for _, op := range ops {
		out = append(out, toAPIOp(op))
	}
	return out, nil
}

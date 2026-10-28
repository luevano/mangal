package remote

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/luevano/libmangal"
	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// unsupported is what the server's API doesn't do yet (docs/remote.md R3/R4).
func unsupported(what string) error {
	return fmt.Errorf("%s isn't on a remote mangal serve yet (mangal without --remote does it): %w", what, errors.ErrUnsupported)
}

// remoteLibrary is the server's library (core.Library).
type remoteLibrary struct{ c *Client }

var _ core.Library = remoteLibrary{}

func (remoteLibrary) Root(context.Context) (string, error) {
	return "", unsupported("the server's download path")
}

func (l remoteLibrary) list(ctx context.Context, params *apiv1.ListLibraryMangasParams) ([]storage.Manga, error) {
	r, err := l.c.API.ListLibraryMangasWithResponse(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	list, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	if err != nil {
		return nil, err
	}
	mangas := make([]storage.Manga, len(list.Mangas))
	for i, m := range list.Mangas {
		mangas[i] = mangaOf(m)
	}
	return mangas, nil
}

// Mangas are the server's mangas; it keeps their statuses itself (its scans), and the
// list doesn't say which folders are gone.
func (l remoteLibrary) Mangas(ctx context.Context, _ core.MangasOptions) (core.MangaList, error) {
	mangas, err := l.list(ctx, nil)
	return core.MangaList{Mangas: mangas}, err
}

func (l remoteLibrary) detail(ctx context.Context, id int64) (apiv1.LibraryMangaDetail, error) {
	r, err := l.c.API.GetLibraryMangaWithResponse(ctx, id)
	if err != nil {
		return apiv1.LibraryMangaDetail{}, fmt.Errorf("mangal serve: %w", err)
	}
	return answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
}

func (l remoteLibrary) Manga(ctx context.Context, id int64) (storage.Manga, error) {
	d, err := l.detail(ctx, id)
	return mangaOf(d.Manga), err
}

func (l remoteLibrary) MangaBySource(ctx context.Context, provider, mangaID string) (storage.Manga, bool, error) {
	mangas, err := l.list(ctx, &apiv1.ListLibraryMangasParams{Provider: &provider})
	if err != nil {
		return storage.Manga{}, false, err
	}
	var found *storage.Manga
	for i, m := range mangas {
		for _, s := range m.Sources {
			if s.Provider == provider && s.MangaID == mangaID && (found == nil || s.Current) {
				found = &mangas[i]
			}
		}
	}
	if found == nil {
		return storage.Manga{}, false, nil
	}
	return *found, true, nil
}

func (remoteLibrary) MangaFor(context.Context, libmangal.ProviderInfo, string, []lmmeta.ID) (storage.Manga, bool, error) {
	return storage.Manga{}, false, unsupported("finding a provider manga's library manga by its metadata ids")
}

func (l remoteLibrary) Downloads(ctx context.Context, manga int64) ([]storage.Download, error) {
	r, err := l.c.API.ListLibraryChaptersWithResponse(ctx, manga)
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	list, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	if err != nil {
		return nil, err
	}
	out := make([]storage.Download, len(list))
	for i, ch := range list {
		out[i] = downloadOf(ch.Download)
	}
	return out, nil
}

func (l remoteLibrary) ProviderDownloads(ctx context.Context, provider, mangaID string) ([]storage.Download, error) {
	r, err := l.c.API.ListDownloadsWithResponse(ctx, &apiv1.ListDownloadsParams{Provider: opt(provider), MangaId: opt(mangaID)})
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	list, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	if err != nil {
		return nil, err
	}
	out := make([]storage.Download, len(list.Downloads))
	for i, d := range list.Downloads {
		out[i] = downloadOf(d)
	}
	// the API's are newest first, the library's by manga and chapter
	slices.SortStableFunc(out, func(a, b storage.Download) int {
		return cmp.Or(cmp.Compare(a.MangaID, b.MangaID), cmp.Compare(a.Chapter, b.Chapter))
	})
	return out, nil
}

func (remoteLibrary) ChapterFiles(context.Context, core.ChapterFilesReq) (core.ChapterFiles, error) {
	return core.ChapterFiles{}, unsupported("a provider manga's chapters in the library")
}

func (remoteLibrary) Links(context.Context, int64) (library.LinkInfo, error) {
	return library.LinkInfo{}, unsupported("the links of a library manga")
}

func (remoteLibrary) LinkPick(context.Context, int64, lmmeta.IDCode) (storage.LinkPick, bool, error) {
	return storage.LinkPick{}, false, unsupported("a link pick")
}

func (l remoteLibrary) Trash(ctx context.Context) ([]library.TrashBatch, error) {
	r, err := l.c.API.GetTrashWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	t, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	if err != nil {
		return nil, err
	}
	out := make([]library.TrashBatch, len(t.Batches))
	for i, b := range t.Batches {
		out[i] = library.TrashBatch{Name: b.Name, Path: b.Path, Files: b.Files, Size: b.Size}
	}
	return out, nil
}

func (l remoteLibrary) Duplicates(ctx context.Context) ([]library.Duplicate, error) {
	r, err := l.c.API.ListDuplicatesWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	list, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	if err != nil {
		return nil, err
	}
	out := make([]library.Duplicate, len(list))
	for i, d := range list {
		dup := library.Duplicate{ID: lmmeta.IDCode(d.Source), Raw: d.Id}
		for _, m := range d.Mangas {
			dup.Mangas = append(dup.Mangas, library.DuplicateManga{Manga: mangaOf(m), Watched: deref(m.Watched)})
		}
		out[i] = dup
	}
	return out, nil
}

// Scan runs the server's scan job and waits for it: the report has only its Summary.
func (l remoteLibrary) Scan(ctx context.Context, opts core.ScanOptions) (core.ScanReport, error) {
	r, err := l.c.API.ScanLibraryWithResponse(ctx, apiv1.ScanRequest{Check: opt(opts.Check), Statuses: opt(opts.Statuses)})
	if err != nil {
		return core.ScanReport{}, fmt.Errorf("mangal serve: %w", err)
	}
	job, err := answer(r.StatusCode(), r.Body, r.JSON202, r.JSONDefault)
	if err != nil {
		return core.ScanReport{}, err
	}
	j, err := l.c.waitJob(ctx, jobOf(job))
	if err != nil {
		return core.ScanReport{}, err
	}
	if j.Status != jobs.Done {
		return core.ScanReport{}, fmt.Errorf("the server's scan (job %d): %s", j.ID, cmp.Or(j.Error, j.Status.String()))
	}
	return core.ScanReport{Summary: j.Message}, nil
}

// ScanManga is the server's manga as it is.
// shortcut: no single-manga scan in the API (R3: POST /library/mangas/{id}/scan), the rows aren't reconciled.
func (l remoteLibrary) ScanManga(ctx context.Context, id int64) (core.MangaScan, error) {
	m, err := l.Manga(ctx, id)
	return core.MangaScan{Manga: m}, err
}

func (remoteLibrary) CheckIDs(context.Context) ([]library.IDCheck, error) {
	return nil, unsupported("checking the folders' ids")
}

func (l remoteLibrary) SourceURL(ctx context.Context, manga int64, provider, mangaID string, refresh bool) (string, error) {
	r, err := l.c.API.GetLibrarySourceURLWithResponse(ctx, manga, &apiv1.GetLibrarySourceURLParams{Provider: provider, MangaId: mangaID, Refresh: opt(refresh)})
	if err != nil {
		return "", fmt.Errorf("mangal serve: %w", err)
	}
	u, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	return u.Url, err
}

func (remoteLibrary) LookupSourceURLs(context.Context, core.SourceURLsOptions) (core.SourceURLsReport, error) {
	return core.SourceURLsReport{}, unsupported("looking up every source's address")
}

// waitJob polls the job until it's over.
func (c *Client) waitJob(ctx context.Context, j core.Job) (core.Job, error) {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for j.Status.Active() {
		if j.Message != "" {
			core.ReportProgress(ctx, library.Progress{Phase: j.Message})
		}
		select {
		case <-ctx.Done():
			return j, ctx.Err()
		case <-tick.C:
		}
		var err error
		if j, err = c.job(ctx, j.ID); err != nil {
			return j, err
		}
	}
	return j, nil
}

var metadataSources = map[lmmeta.IDCode]lmmeta.IDSource{
	lmmeta.IDCodeAnilist: lmmeta.IDSourceAnilist, lmmeta.IDCodeMyAnimeList: lmmeta.IDSourceMyAnimeList,
	lmmeta.IDCodeKitsu: lmmeta.IDSourceKitsu, lmmeta.IDCodeMangaUpdates: lmmeta.IDSourceMangaUpdates,
	lmmeta.IDCodeAnimePlanet: lmmeta.IDSourceAnimePlanet,
}

// mangaOf is the API's library manga as the library's row.
func mangaOf(m apiv1.LibraryManga) storage.Manga {
	out := storage.Manga{ID: m.Id, Title: m.Title, Parent: m.Path, Chapters: m.Chapters, Status: deref(m.Status),
		StatusUpdated: deref(m.StatusUpdated), Hold: deref(m.Hold), NewProvider: deref(m.NewProvider)}
	if dir := deref(m.Dir); dir != "" && strings.HasSuffix(m.Path, dir) {
		out.Parent, out.Dir = strings.TrimRight(strings.TrimSuffix(m.Path, dir), `/\`), dir
	}
	for _, id := range m.MetadataIds {
		code := lmmeta.IDCode(id.Source)
		out.MetadataIDs = append(out.MetadataIDs, lmmeta.ID{Raw: id.Id, Code: code, Source: metadataSources[code]})
	}
	for _, s := range m.Sources {
		out.Sources = append(out.Sources, storage.Source{Provider: s.Provider, MangaID: s.MangaId, Title: s.Title,
			URL: deref(s.Url), Gone: deref(s.Gone), Moved: deref(s.Moved), Current: s.Current})
	}
	return out
}

func downloadOf(d apiv1.Download) storage.Download {
	return storage.Download{Path: d.Path, Provider: d.Provider, MangaID: d.MangaId, MangaTitle: d.MangaTitle,
		Volume: d.Volume, Chapter: d.Chapter, ChapterTitle: d.ChapterTitle, ChapterURL: deref(d.ChapterUrl),
		Format: d.Format, Pages: d.Pages, Downloaded: d.Downloaded, TitleProvider: deref(d.TitleProvider),
		TitleMangaID: deref(d.TitleMangaId), TitleChanged: deref(d.TitleChanged)}
}

// remoteBindings are the server's bindings (core.Bindings).
type remoteBindings struct{ c *Client }

func (b remoteBindings) List(ctx context.Context, provider, mangaID string) ([]storage.Binding, error) {
	r, err := b.c.API.ListBindingsWithResponse(ctx, &apiv1.ListBindingsParams{Provider: opt(provider), MangaId: opt(mangaID)})
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	return answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
}

func (b remoteBindings) OfManga(ctx context.Context, id int64) ([]storage.Binding, error) {
	d, err := remoteLibrary(b).detail(ctx, id)
	return d.Bindings, err
}

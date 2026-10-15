package serve

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/library/pages"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/web"
	"github.com/luevano/mangal/v5/web/apiv1"
	"github.com/spf13/afero"
)

// pageCache is how long the browser keeps a page or a cover.
const pageCache = "private, max-age=3600"

// covers are the cover files of a manga folder, the first found is served.
var covers = []string{metadata.FilenameCoverJPG, "cover.jpeg", "cover.png", "cover.webp"}

// GetLibraryCover implements apiv1.StrictServerInterface.
func (a *API) GetLibraryCover(ctx context.Context, request apiv1.GetLibraryCoverRequestObject) (apiv1.GetLibraryCoverResponseObject, error) {
	c, err := a.services(ctx)
	if err != nil {
		return nil, err
	}
	m, err := c.Library.Manga(request.Id)
	if err != nil {
		return nil, err
	}
	fsys := c.Env().FS
	for _, name := range covers {
		p := filepath.Join(m.Path(), name)
		data, err := afero.ReadFile(fsys, p)
		if err != nil {
			continue
		}
		return apiv1.GetLibraryCover200ImageResponse{Body: bytes.NewReader(data), ContentType: mime.TypeByExtension(filepath.Ext(name)),
			ContentLength: int64(len(data)), Headers: apiv1.GetLibraryCover200ResponseHeaders{CacheControl: ptr(pageCache)}}, nil
	}
	return nil, newAPIError(http.StatusNotFound, watch.CodeNotFound, "%s has no cover", m.Title)
}

// libraryChapter is the downloaded chapter of the library manga at path: only a file
// the library recorded as the manga's download is read.
func (a *API) libraryChapter(ctx context.Context, id int64, path string) (*pages.Chapter, storage.Download, error) {
	c, err := a.services(ctx)
	if err != nil {
		return nil, storage.Download{}, err
	}
	m, err := c.Library.Manga(id)
	if err != nil {
		return nil, storage.Download{}, err
	}
	downloads, err := c.Library.Downloads(m.ID)
	if err != nil {
		return nil, storage.Download{}, err
	}
	i := slices.IndexFunc(downloads, func(d storage.Download) bool { return d.Path == path })
	if i < 0 {
		return nil, storage.Download{}, newAPIError(http.StatusNotFound, watch.CodeNotFound, "%s has no chapter %q", m.Title, path)
	}
	ch, err := pages.Open(c.Env().FS, path)
	if err != nil {
		return nil, downloads[i], unprocessable(err)
	}
	return ch, downloads[i], nil
}

func toAPIPages(format string, list []pages.Page) apiv1.ChapterPages {
	out := apiv1.ChapterPages{Format: format, Pages: []apiv1.ChapterPage{}}
	for i, p := range list {
		out.Pages = append(out.Pages, apiv1.ChapterPage{Index: i, Name: ptr(p.Name), Size: ptr(p.Size), Type: ptr(p.Type())})
	}
	return out
}

// ListChapterPages implements apiv1.StrictServerInterface.
func (a *API) ListChapterPages(ctx context.Context, request apiv1.ListChapterPagesRequestObject) (apiv1.ListChapterPagesResponseObject, error) {
	ch, _, err := a.libraryChapter(ctx, request.Id, request.Params.Path)
	if err != nil {
		return nil, err
	}
	out := toAPIPages(ch.Format.String(), ch.Pages)
	if ch.Format == libmangal.FormatPDF {
		out.Pdf = ptr(true)
	}
	return apiv1.ListChapterPages200JSONResponse(out), nil
}

// GetChapterPage implements apiv1.StrictServerInterface.
func (a *API) GetChapterPage(ctx context.Context, request apiv1.GetChapterPageRequestObject) (apiv1.GetChapterPageResponseObject, error) {
	ch, _, err := a.libraryChapter(ctx, request.Id, request.Params.Path)
	if err != nil {
		return nil, err
	}
	r, p, err := ch.Read(request.Page)
	switch {
	case errors.Is(err, pages.ErrNotFound):
		return nil, newAPIError(http.StatusNotFound, watch.CodeNotFound, "no page %d (%d pages)", request.Page, len(ch.Pages))
	case errors.Is(err, pages.ErrNoPages):
		return nil, badRequest("%s", err)
	case err != nil:
		return nil, err
	}
	// the strict server copies the body, then closes nothing
	data, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		return nil, fmt.Errorf("read page %d of %s: %w", request.Page, filepath.Base(ch.Path), err)
	}
	return apiv1.GetChapterPage200ImageResponse{Body: bytes.NewReader(data), ContentType: p.Type(), ContentLength: int64(len(data)),
		Headers: apiv1.GetChapterPage200ResponseHeaders{CacheControl: ptr(pageCache)}}, nil
}

// GetChapterFile implements apiv1.StrictServerInterface: the file streamed (a folder
// zipped on the way).
func (a *API) GetChapterFile(ctx context.Context, request apiv1.GetChapterFileRequestObject) (apiv1.GetChapterFileResponseObject, error) {
	ch, _, err := a.libraryChapter(ctx, request.Id, request.Params.Path)
	if err != nil {
		return nil, err
	}
	var size int64
	if ch.Format != libmangal.FormatImages {
		if info, err := ch.FS.Stat(ch.Path); err == nil {
			size = info.Size()
		}
	}
	pr, pw := io.Pipe()
	go func() { pw.CloseWithError(ch.WriteFile(pw)) }()
	disposition := "attachment"
	if deref(request.Params.Inline) {
		disposition = "inline"
	}
	disposition = mime.FormatMediaType(disposition, map[string]string{"filename": ch.FileName()})
	return apiv1.GetChapterFile200AsteriskResponse{Body: pr, ContentType: ch.ContentType(), ContentLength: size,
		Headers: apiv1.GetChapterFile200ResponseHeaders{ContentDisposition: &disposition}}, nil
}

// onlinePages keeps the pages of the providers' chapters read lately, so each page
// request doesn't list them again.
type onlinePages struct {
	mu      sync.Mutex
	entries map[string]onlineEntry
}

type onlineEntry struct {
	pages []mangadata.Page
	at    time.Time
}

const (
	onlinePagesKept = 32
	onlinePagesTTL  = 30 * time.Minute
)

func (o *onlinePages) get(key string) ([]mangadata.Page, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	e, ok := o.entries[key]
	if !ok || time.Since(e.at) > onlinePagesTTL {
		return nil, false
	}
	return e.pages, true
}

func (o *onlinePages) add(key string, list []mangadata.Page) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.entries == nil {
		o.entries = map[string]onlineEntry{}
	}
	for len(o.entries) >= onlinePagesKept {
		oldest := ""
		for k, e := range o.entries {
			if oldest == "" || e.at.Before(o.entries[oldest].at) {
				oldest = k
			}
		}
		delete(o.entries, oldest)
	}
	o.entries[key] = onlineEntry{pages: list, at: time.Now()}
}

func onlineKey(provider, mangaID string, volume *float32, chapter float32) string {
	v := "any"
	if volume != nil {
		v = fmt.Sprint(*volume)
	}
	return fmt.Sprintf("%s\x00%s\x00%s\x00%g", provider, mangaID, v, chapter)
}

// providerPages are the pages of the provider's chapter, listed through the client c.
func (a *API) providerPages(ctx context.Context, c *libmangal.Client, mangaID string, volume *float32, chapter float32) ([]mangadata.Page, error) {
	key := onlineKey(c.Info().ID, mangaID, volume, chapter)
	if list, ok := a.online.get(key); ok {
		return list, nil
	}
	m, err := providerManga(ctx, c, mangaID, "")
	if err != nil {
		return nil, err
	}
	chapters, err := client.MangaChapters(ctx, c, m)
	if err != nil {
		return nil, unprocessable(err)
	}
	i := slices.IndexFunc(chapters, func(ch mangadata.Chapter) bool {
		return ch.Info().Number == chapter && (volume == nil || ch.Volume().Info().Number == *volume)
	})
	if i < 0 {
		return nil, newAPIError(http.StatusNotFound, watch.CodeNotFound, "%s has no chapter %g", m.Info().Title, chapter)
	}
	list, err := c.ChapterPagesContext(ctx, chapters[i])
	if err != nil {
		return nil, unprocessable(fmt.Errorf("get pages of %q: %w", chapters[i], err))
	}
	a.online.add(key, list)
	return list, nil
}

// ListProviderPages implements apiv1.StrictServerInterface.
func (a *API) ListProviderPages(ctx context.Context, request apiv1.ListProviderPagesRequestObject) (apiv1.ListProviderPagesResponseObject, error) {
	p := request.Params
	return withClient(ctx, a, request.Provider, func(c *libmangal.Client) (apiv1.ListProviderPagesResponseObject, error) {
		list, err := a.providerPages(ctx, c, p.MangaId, p.Volume, p.Chapter)
		if err != nil {
			return nil, err
		}
		out := apiv1.ChapterPages{Format: "ONLINE", Pages: []apiv1.ChapterPage{}}
		for i, pg := range list {
			out.Pages = append(out.Pages, apiv1.ChapterPage{Index: i, Name: ptr(fmt.Sprintf("%04d%s", i+1, pg.Extension()))})
		}
		return apiv1.ListProviderPages200JSONResponse(out), nil
	})
}

// GetProviderPage implements apiv1.StrictServerInterface: the page through the provider
// (its headers and rate limit), kept in the image proxy's cache.
func (a *API) GetProviderPage(ctx context.Context, request apiv1.GetProviderPageRequestObject) (apiv1.GetProviderPageResponseObject, error) {
	p := request.Params
	return withClient(ctx, a, request.Provider, func(c *libmangal.Client) (apiv1.GetProviderPageResponseObject, error) {
		key := "page\x00" + onlineKey(c.Info().ID, p.MangaId, p.Volume, p.Chapter) + fmt.Sprintf("\x00%d", request.Page)
		cached := web.UncachedImage
		if a.Browser != nil {
			cached = a.Browser.CachedImage
		}
		data, contentType, err := cached(key, func() ([]byte, error) {
			list, err := a.providerPages(ctx, c, p.MangaId, p.Volume, p.Chapter)
			if err != nil {
				return nil, err
			}
			if request.Page >= len(list) {
				return nil, newAPIError(http.StatusNotFound, watch.CodeNotFound, "no page %d (%d pages)", request.Page, len(list))
			}
			page, err := c.DownloadPageContext(ctx, list[request.Page])
			if err != nil {
				return nil, unprocessable(fmt.Errorf("get page %d: %w", request.Page+1, err))
			}
			return page.Image(), nil
		})
		if err != nil {
			return nil, err
		}
		return apiv1.GetProviderPage200ImageResponse{Body: bytes.NewReader(data), ContentType: contentType, ContentLength: int64(len(data)),
			Headers: apiv1.GetProviderPage200ResponseHeaders{CacheControl: ptr(pageCache)}}, nil
	})
}

// syncProgress is client.SyncProgress, swapped in tests.
var syncProgress, defaultSyncProgress = client.SyncProgress, client.SyncProgress

// RecordRead implements apiv1.StrictServerInterface.
func (a *API) RecordRead(ctx context.Context, request apiv1.RecordReadRequestObject) (apiv1.RecordReadResponseObject, error) {
	b := deref(request.Body)
	provider, mangaID := deref(b.Provider), deref(b.MangaId)
	if (provider == "" || mangaID == "") && deref(b.LibraryManga) == 0 {
		return nil, badRequest("provider and manga_id, or library_manga, are needed")
	}
	svc, err := a.services(ctx)
	if err != nil {
		return nil, err
	}
	st := svc.Env().Storage
	libraryManga := deref(b.LibraryManga)
	var ids map[metadata.IDCode]string
	mangas, err := st.Library().Mangas()
	if err != nil {
		return nil, err
	}
	for _, m := range mangas {
		if m.ID == libraryManga || (libraryManga == 0 && slices.ContainsFunc(m.Sources, func(s storage.Source) bool {
			return s.Provider == provider && s.MangaID == mangaID
		})) {
			libraryManga = m.ID
			ids = map[metadata.IDCode]string{}
			for _, id := range m.MetadataIDs {
				ids[id.Code] = id.Raw
			}
			break
		}
	}
	out := apiv1.RecordRead200JSONResponse{Synced: []string{}}
	switch {
	case !config.Read.History.Local.Get():
		out.Note = ptr("read.history.local is off")
	case provider == "" || mangaID == "":
		// the history is by provider and manga id
		out.Note = ptr("the chapter has no provider manga (a scanned folder): synced only")
	default:
		title := deref(b.MangaTitle)
		if title == "" {
			title = mangaID
		}
		err := st.History().Record(storage.Read{Provider: provider, MangaID: mangaID, MangaTitle: title,
			Volume: deref(b.Volume), Chapter: b.Chapter, ChapterTitle: deref(b.ChapterTitle), ChapterURL: deref(b.ChapterUrl),
			LibraryManga: libraryManga, Last: time.Now()})
		if err != nil {
			return nil, err
		}
		out.Recorded = true
	}
	if b.Sync == nil || *b.Sync {
		var bindings []storage.Binding
		if provider != "" && mangaID != "" {
			if bindings, err = st.Bindings().Get(provider, mangaID); err != nil {
				return nil, err
			}
		}
		synced, err := syncProgress(ctx, bindings, ids, b.Chapter)
		out.Synced = append(out.Synced, synced...)
		if err != nil {
			out.Error = ptr(err.Error())
		}
	}
	a.Log.Infof("read %s %q chapter %g (library manga %d) through the API%s", provider, mangaID, b.Chapter, libraryManga, syncedNote(out.Synced))
	return out, nil
}

func syncedNote(synced []string) string {
	if len(synced) == 0 {
		return ""
	}
	return " (synced to " + strings.Join(synced, ", ") + ")"
}

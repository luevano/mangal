package serve

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/netutil"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/selector"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// ListLogs implements apiv1.StrictServerInterface: the last limit entries (200 by
// default) of every file (or the one named), oldest first.
func (a *API) ListLogs(ctx context.Context, request apiv1.ListLogsRequestObject) (apiv1.ListLogsResponseObject, error) {
	p := request.Params
	q := core.LogQuery{Level: deref(p.Level), Grep: deref(p.Grep), Limit: deref(p.Limit)}
	if p.Limit == nil {
		q.Limit = 200
	}
	var err error
	if s := deref(p.Since); s != "" {
		if q.Since, err = parseSince(s); err != nil {
			return nil, badRequest("since: %w", err)
		}
	}
	if s := deref(p.Until); s != "" {
		if q.Until, err = parseSince(s); err != nil {
			return nil, badRequest("until: %w", err)
		}
	}
	if name := deref(p.File); name != "" {
		q.Files = []string{name}
	}
	c, err := a.services(ctx)
	if err != nil {
		return nil, err
	}
	entries := []apiv1.LogEntry{}
	err = c.Logs.Entries(ctx, q, func(e log.Entry) error {
		entries = append(entries, toAPILog(e))
		return nil
	})
	if errors.Is(err, core.ErrNotFound) {
		return nil, fmt.Errorf("%w (see /api/v1/logs/files)", err)
	}
	if err != nil {
		return nil, fmt.Errorf("read logs: %w", err)
	}
	return apiv1.ListLogs200JSONResponse(entries), nil
}

// ListLogFiles implements apiv1.StrictServerInterface.
func (a *API) ListLogFiles(ctx context.Context, _ apiv1.ListLogFilesRequestObject) (apiv1.ListLogFilesResponseObject, error) {
	c, err := a.services(ctx)
	if err != nil {
		return nil, err
	}
	files, err := c.Logs.Files(ctx)
	if err != nil {
		return nil, err
	}
	out := apiv1.ListLogFiles200JSONResponse{}
	for _, f := range files {
		out = append(out, apiv1.LogFile{Name: f.Name, Path: f.Path, Size: f.Size, Modified: f.ModTime, Closed: f.Closed, Compressed: f.Compressed})
	}
	return out, nil
}

func toAPIManga(m mangadata.Manga) apiv1.Manga {
	info := m.Info()
	out := apiv1.Manga{Id: info.ID, Title: info.Title}
	for _, f := range []struct {
		v string
		p **string
	}{{info.URL, &out.Url}, {info.Cover, &out.Cover}, {info.Banner, &out.Banner}} {
		if f.v != "" {
			v := f.v
			*f.p = &v
		}
	}
	return out
}

// ListProviders implements apiv1.StrictServerInterface.
func (a *API) ListProviders(ctx context.Context, _ apiv1.ListProvidersRequestObject) (apiv1.ListProvidersResponseObject, error) {
	infos, err := core.NewProviders(a.loaders).List(ctx)
	if err != nil {
		return nil, err
	}
	out := apiv1.ListProviders200JSONResponse{}
	for _, info := range infos {
		out = append(out, toAPIProvider(info))
	}
	return out, nil
}

// GetProviderInfo implements apiv1.StrictServerInterface.
func (a *API) GetProviderInfo(ctx context.Context, request apiv1.GetProviderInfoRequestObject) (apiv1.GetProviderInfoResponseObject, error) {
	info, err := core.NewProviders(a.loaders).Info(ctx, request.Provider)
	if err != nil {
		return nil, err
	}
	return apiv1.GetProviderInfo200JSONResponse(toAPIProvider(info)), nil
}

// iconCache is how long the browser keeps a site's icon.
const iconCache = "private, max-age=86400"

// GetProviderIcon implements apiv1.StrictServerInterface.
func (a *API) GetProviderIcon(ctx context.Context, request apiv1.GetProviderIconRequestObject) (apiv1.GetProviderIconResponseObject, error) {
	if a.Browser == nil {
		return nil, newAPIError(http.StatusServiceUnavailable, CodeUnavailable, "no image proxy here")
	}
	info, err := core.NewProviders(a.loaders).Info(ctx, request.Provider)
	if err != nil {
		return nil, err
	}
	data, contentType, err := a.Browser.Icon(ctx, info.Icon, info.Website)
	if err != nil {
		return nil, err
	}
	return apiv1.GetProviderIcon200ImageResponse{Body: bytes.NewReader(data), ContentType: contentType, ContentLength: int64(len(data)),
		Headers: apiv1.GetProviderIcon200ResponseHeaders{CacheControl: new(iconCache)}}, nil
}

// withClient runs fn with the provider's client.
func withClient[T any](ctx context.Context, a *API, provider string, fn func(*libmangal.Client) (T, error)) (T, error) {
	var zero T
	l, err := a.loader(provider)
	if err != nil {
		return zero, err
	}
	c, release, err := client.Hold(ctx, l)
	if err != nil {
		return zero, unprocessable(err)
	}
	defer release()
	return fn(c)
}

// SearchProvider implements apiv1.StrictServerInterface.
func (a *API) SearchProvider(ctx context.Context, request apiv1.SearchProviderRequestObject) (apiv1.SearchProviderResponseObject, error) {
	id, q := deref(request.Params.Id), deref(request.Params.Q)
	if id == "" && q == "" {
		return nil, badRequest("q or id is needed")
	}
	warnIDQuery(q)
	return withClient(ctx, a, request.Provider, func(c *libmangal.Client) (apiv1.SearchProviderResponseObject, error) {
		mangas, err := selector.Search(ctx, client.Searcher(c), id, q)
		if errors.Is(err, libmangal.ErrMangaByIDUnsupported) {
			return nil, unprocessable(err)
		}
		if err != nil {
			return nil, err
		}
		out := apiv1.SearchProvider200JSONResponse{}
		for _, m := range mangas {
			out = append(out, toAPIManga(m))
		}
		return out, nil
	})
}

// warnIDQuery logs a deprecated "mid:<id>" query.
func warnIDQuery(q string) {
	if id, ok := selector.IDQuery(q); ok {
		log.Warn(`search: "mid:" queries are deprecated, use id=%s`, id)
	}
}

// providerManga finds the provider's manga by its id (or a search of q, when the
// provider can't get one by id).
func providerManga(ctx context.Context, c *libmangal.Client, id, q string) (mangadata.Manga, error) {
	m, err := selector.Find(ctx, client.Searcher(c), id, q, "")
	if err != nil {
		return nil, providerError(err)
	}
	return m, nil
}

// providerError is a provider's error: a manga it doesn't have (any more) is a 404, the
// rest a 422 (rate limits stay 429s).
func providerError(err error) error {
	if _, limited := netutil.RetryAfterOf(err); limited || errors.Is(err, libmangal.ErrNotFound) {
		return err
	}
	return unprocessable(err)
}

// GetProviderManga implements apiv1.StrictServerInterface.
func (a *API) GetProviderManga(ctx context.Context, request apiv1.GetProviderMangaRequestObject) (apiv1.GetProviderMangaResponseObject, error) {
	return withClient(ctx, a, request.Provider, func(c *libmangal.Client) (apiv1.GetProviderMangaResponseObject, error) {
		m, err := providerManga(ctx, c, request.Params.MangaId, deref(request.Params.Q))
		if err != nil {
			return nil, err
		}
		provider, mangaID := c.Info().ID, m.Info().ID
		out := apiv1.GetProviderManga200JSONResponse{Provider: provider, Manga: toAPIManga(m), Bindings: []storage.Binding{}}
		svc, err := a.services(ctx)
		if err != nil {
			return nil, err
		}
		st := svc.Env().Storage
		if out.Bindings, err = st.Bindings().Get(provider, mangaID); err != nil {
			return nil, err
		}
		out.Bindings = orEmpty(out.Bindings)
		watches, err := a.Manager().List(ctx)
		if err != nil {
			return nil, err
		}
		for _, w := range watches {
			if w.Provider == provider && w.MangaID == mangaID {
				out.Watch = &w.ID
				if w.LibraryManga != 0 {
					out.Library = &w.LibraryManga
				}
			}
		}
		if out.Library == nil {
			mangas, err := st.Library().Mangas()
			if err != nil {
				return nil, err
			}
			for _, lm := range mangas {
				if slices.ContainsFunc(lm.Sources, func(s storage.Source) bool { return s.Provider == provider && s.MangaID == mangaID }) {
					out.Library = &lm.ID
					break
				}
			}
		}
		return out, nil
	})
}

// ListProviderChapters implements apiv1.StrictServerInterface.
func (a *API) ListProviderChapters(ctx context.Context, request apiv1.ListProviderChaptersRequestObject) (apiv1.ListProviderChaptersResponseObject, error) {
	return withClient(ctx, a, request.Provider, func(c *libmangal.Client) (apiv1.ListProviderChaptersResponseObject, error) {
		m, err := providerManga(ctx, c, request.Params.MangaId, deref(request.Params.Q))
		if err != nil {
			return nil, err
		}
		chapters, err := client.MangaChapters(ctx, c, m)
		if err != nil {
			return nil, providerError(err)
		}
		svc, err := a.services(ctx)
		if err != nil {
			return nil, err
		}
		st := svc.Env().Storage
		provider, mangaID := c.Info().ID, m.Info().ID
		downloads, err := st.Library().ListDownloads(provider, mangaID)
		if err != nil {
			return nil, err
		}
		paths := map[float32]string{}
		for _, d := range downloads {
			paths[d.Chapter] = d.Path
		}
		reads, err := st.History().List(provider, mangaID)
		if err != nil {
			return nil, err
		}
		counts := map[float32]int{}
		for _, r := range reads {
			counts[r.Chapter] += r.Count
		}
		out := apiv1.ListProviderChapters200JSONResponse{}
		for _, ch := range chapters {
			info := ch.Info()
			pc := apiv1.ProviderChapter{Volume: ch.Volume().Info().Number, Number: info.Number, Title: info.Title, Reads: counts[info.Number]}
			if info.URL != "" {
				pc.Url = &info.URL
			}
			if info.Unavailable != "" {
				pc.Unavailable = &info.Unavailable
			}
			if p, ok := paths[info.Number]; ok {
				pc.Downloaded, pc.Path = true, &p
			}
			out = append(out, pc)
		}
		return out, nil
	})
}

// ListFormats implements apiv1.StrictServerInterface.
func (a *API) ListFormats(context.Context, apiv1.ListFormatsRequestObject) (apiv1.ListFormatsResponseObject, error) {
	out := apiv1.ListFormats200JSONResponse{}
	for _, f := range libmangal.FormatValues() {
		format := apiv1.Format{Name: f.String()}
		if ext := f.Extension(); ext != "" {
			format.Extension = &ext
		}
		out = append(out, format)
	}
	return out, nil
}

// ProxyImage implements apiv1.StrictServerInterface.
func (a *API) ProxyImage(ctx context.Context, request apiv1.ProxyImageRequestObject) (apiv1.ProxyImageResponseObject, error) {
	if a.Browser == nil {
		return nil, newAPIError(http.StatusServiceUnavailable, CodeUnavailable, "no image proxy here")
	}
	data, contentType, err := a.Browser.Image(ctx, request.Params.Url, deref(request.Params.Referer))
	if err != nil {
		return nil, err
	}
	return apiv1.ProxyImage200ImageResponse{Body: bytes.NewReader(data), ContentType: contentType, ContentLength: int64(len(data))}, nil
}

// ListBindings implements apiv1.StrictServerInterface.
func (a *API) ListBindings(ctx context.Context, request apiv1.ListBindingsRequestObject) (apiv1.ListBindingsResponseObject, error) {
	svc, err := a.services(ctx)
	if err != nil {
		return nil, err
	}
	list, err := svc.Library.Bindings(ctx, deref(request.Params.Provider), deref(request.Params.MangaId))
	if err != nil {
		return nil, err
	}
	return apiv1.ListBindings200JSONResponse(orEmpty(list)), nil
}

// ListHistory implements apiv1.StrictServerInterface.
func (a *API) ListHistory(ctx context.Context, request apiv1.ListHistoryRequestObject) (apiv1.ListHistoryResponseObject, error) {
	svc, err := a.services(ctx)
	if err != nil {
		return nil, err
	}
	p := request.Params
	reads, err := svc.Library.History(ctx, deref(p.Provider), deref(p.MangaId))
	if err != nil {
		return nil, err
	}
	list := make([]apiv1.Read, 0, len(reads))
	for _, r := range reads {
		read := apiv1.Read{Provider: r.Provider, MangaId: r.MangaID, MangaTitle: r.MangaTitle, Volume: r.Volume, Chapter: r.Chapter,
			ChapterTitle: r.ChapterTitle, Count: r.Count, First: r.First, Last: r.Last}
		if r.ChapterURL != "" {
			read.ChapterUrl = &r.ChapterURL
		}
		if r.LibraryManga != 0 {
			read.LibraryManga = &r.LibraryManga
		}
		list = append(list, read)
	}
	return apiv1.ListHistory200JSONResponse{Total: len(list), Reads: page(list, p.Limit, p.Offset)}, nil
}

// applies is when a change of the key applies in the server: the watch.* keys are re-read
// every cycle, the rest at the next start.
func applies(key string) string {
	if slices.Contains(watchKeys, key) {
		return "now"
	}
	return "restart"
}

func toAPIConfig(e core.ConfigEntry) apiv1.ConfigEntry {
	out := apiv1.ConfigEntry{Key: e.Key, Value: e.Value, Default: e.Default, Description: e.Description, Secret: e.Secret, Applies: applies(e.Key)}
	if e.Env != "" {
		out.Env = &e.Env
	}
	return out
}

// ListConfig implements apiv1.StrictServerInterface.
func (a *API) ListConfig(ctx context.Context, _ apiv1.ListConfigRequestObject) (apiv1.ListConfigResponseObject, error) {
	entries, err := core.NewConfig().List(ctx)
	if err != nil {
		return nil, err
	}
	out := apiv1.ListConfig200JSONResponse{}
	for _, e := range entries {
		out = append(out, toAPIConfig(e))
	}
	return out, nil
}

// adminConfigKeys are set over HTTP only by an admin: where mangal keeps its data and
// runs providers from, which addresses the providers may reach.
var adminConfigKeys = []string{"providers.allow_private_addresses", "providers.private_allowlist", "data.path", "providers.path", "cache.path"}

// SetConfig implements apiv1.StrictServerInterface: writes the key to the config file.
// Secrets and the serve.* keys (its own security) aren't set over HTTP, adminConfigKeys
// only by an admin.
func (a *API) SetConfig(ctx context.Context, request apiv1.SetConfigRequestObject) (apiv1.SetConfigResponseObject, error) {
	key := request.Key
	if !config.Exists(key) {
		return nil, newAPIError(http.StatusNotFound, "not_found", "no config key %q", key)
	}
	if config.IsSecret(key) || strings.HasPrefix(key, "serve.") {
		return nil, newAPIError(http.StatusForbidden, CodeForbidden, "%s isn't set over HTTP (mangal config set, or the config file)", key)
	}
	if slices.Contains(adminConfigKeys, key) {
		if err := needAdmin(ctx); err != nil {
			return nil, err
		}
	}
	body := deref(request.Body)
	if body.Value == nil {
		return nil, badRequest("value is needed")
	}
	e, err := core.NewConfig().Set(ctx, key, body.Value)
	if errors.Is(err, core.ErrInvalid) {
		return nil, badRequest("%w", err)
	}
	if err != nil {
		return nil, err
	}
	a.Log.Infof("config %s = %v set through the API (applies: %s)", key, e.Value, applies(key))
	return apiv1.SetConfig200JSONResponse(toAPIConfig(e)), nil
}

// ListAccounts implements apiv1.StrictServerInterface.
func (a *API) ListAccounts(ctx context.Context, _ apiv1.ListAccountsRequestObject) (apiv1.ListAccountsResponseObject, error) {
	accounts, err := core.NewAccounts().List(ctx, core.AccountsOptions{Cached: true})
	if err != nil {
		return nil, err
	}
	list := apiv1.ListAccounts200JSONResponse{}
	for _, e := range accounts {
		acc := apiv1.Account{Service: e.Service, Status: e.Status, Checked: optTime(e.Checked), Expires: optTime(e.Expires)}
		for _, f := range []struct {
			v string
			p **string
		}{{e.Provider, &acc.Provider}, {e.User, &acc.User}, {e.URL, &acc.Url}, {e.Error, &acc.Error}} {
			if f.v != "" {
				v := f.v
				*f.p = &v
			}
		}
		if len(e.Details) != 0 {
			acc.Details = &e.Details
		}
		list = append(list, acc)
	}
	return list, nil
}

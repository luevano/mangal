package serve

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/selector"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// ListLogs implements apiv1.StrictServerInterface: the last limit entries (200 by
// default) of every file (or the one named), oldest first.
func (a *API) ListLogs(ctx context.Context, request apiv1.ListLogsRequestObject) (apiv1.ListLogsResponseObject, error) {
	p := request.Params
	var filter log.Filter
	var err error
	if s := deref(p.Since); s != "" {
		if filter.Since, err = parseSince(s); err != nil {
			return nil, badRequest("since: %w", err)
		}
	}
	if s := deref(p.Until); s != "" {
		if filter.Until, err = parseSince(s); err != nil {
			return nil, badRequest("until: %w", err)
		}
	}
	if s := deref(p.Level); s != "" {
		if filter.Level, err = log.ParseLevel(s); err != nil {
			return nil, badRequest("level: %w", err)
		}
	}
	filter.Pattern = log.Pattern(deref(p.Grep), false)
	files, err := log.Files(path.Logs())
	if err != nil {
		return nil, fmt.Errorf("list log files: %w", err)
	}
	if name := deref(p.File); name != "" {
		i := slices.IndexFunc(files, func(f log.File) bool { return f.Name == name || f.Name == name+".gz" })
		if i < 0 {
			return nil, &core.NotFoundError{What: fmt.Sprintf("no log file %q (see /api/v1/logs/files)", name)}
		}
		files = files[i : i+1]
	}
	limit := deref(p.Limit)
	if p.Limit == nil {
		limit = 200
	}
	var entries []apiv1.LogEntry
	err = log.Scan(ctx, files, filter, func(e log.Entry) error {
		entries = append(entries, toAPILog(e))
		if limit > 0 && len(entries) > 2*limit {
			entries = slices.Delete(entries, 0, len(entries)-limit)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read logs: %w", err)
	}
	if limit > 0 && len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	return apiv1.ListLogs200JSONResponse(orEmpty(entries)), nil
}

// ListLogFiles implements apiv1.StrictServerInterface.
func (a *API) ListLogFiles(context.Context, apiv1.ListLogFilesRequestObject) (apiv1.ListLogFilesResponseObject, error) {
	files, err := log.Files(path.Logs())
	if err != nil {
		return nil, fmt.Errorf("list log files: %w", err)
	}
	out := apiv1.ListLogFiles200JSONResponse{}
	for _, f := range files {
		out = append(out, apiv1.LogFile{Name: f.Name, Path: f.Path, Size: f.Size, Modified: f.ModTime, Closed: f.Closed, Compressed: f.Compressed})
	}
	return out, nil
}

func toAPIProvider(l libmangal.ProviderLoader) apiv1.Provider {
	info := l.Info()
	out := apiv1.Provider{Id: info.ID, Name: info.Name, Version: info.Version}
	if info.Description != "" {
		out.Description = &info.Description
	}
	return out
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
func (a *API) ListProviders(context.Context, apiv1.ListProvidersRequestObject) (apiv1.ListProvidersResponseObject, error) {
	loaders, err := a.loaders()
	if err != nil {
		return nil, err
	}
	out := apiv1.ListProviders200JSONResponse{}
	for _, l := range loaders {
		out = append(out, toAPIProvider(l))
	}
	return out, nil
}

// GetProviderInfo implements apiv1.StrictServerInterface.
func (a *API) GetProviderInfo(_ context.Context, request apiv1.GetProviderInfoRequestObject) (apiv1.GetProviderInfoResponseObject, error) {
	l, err := a.loader(request.Provider)
	if err != nil {
		return nil, err
	}
	return apiv1.GetProviderInfo200JSONResponse(toAPIProvider(l)), nil
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
	return withClient(ctx, a, request.Provider, func(c *libmangal.Client) (apiv1.SearchProviderResponseObject, error) {
		mangas, err := c.SearchMangasContext(ctx, request.Params.Q)
		if err != nil {
			return nil, fmt.Errorf("search %q: %w", request.Params.Q, err)
		}
		out := apiv1.SearchProvider200JSONResponse{}
		for _, m := range mangas {
			out = append(out, toAPIManga(m))
		}
		return out, nil
	})
}

// providerManga finds the provider's manga by its id (or a search of q, when the
// provider can't get one by id).
func providerManga(ctx context.Context, c *libmangal.Client, id, q string) (mangadata.Manga, error) {
	m, err := selector.Find(ctx, client.Searcher(c), id, q, "")
	if err != nil {
		return nil, unprocessable(err)
	}
	return m, nil
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
			return nil, err
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
	list, err := svc.Env().Storage.Bindings().List(deref(request.Params.Provider), deref(request.Params.MangaId))
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
	reads, err := svc.Library.History(deref(p.Provider), deref(p.MangaId))
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

func configEntry(key string) (apiv1.ConfigEntry, error) {
	value, err := config.Get(key)
	if err != nil {
		return apiv1.ConfigEntry{}, err
	}
	def, _ := config.Default(key)
	description, _ := config.Description(key)
	e := apiv1.ConfigEntry{Key: key, Value: config.Masked(key, value), Default: config.Masked(key, def), Description: description,
		Secret: config.IsSecret(key), Applies: applies(key)}
	if name, set := config.EnvVar(key); set {
		e.Env = &name
	}
	return e, nil
}

// ListConfig implements apiv1.StrictServerInterface.
func (a *API) ListConfig(context.Context, apiv1.ListConfigRequestObject) (apiv1.ListConfigResponseObject, error) {
	out := apiv1.ListConfig200JSONResponse{}
	for _, key := range config.Keys() {
		e, err := configEntry(key)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// SetConfig implements apiv1.StrictServerInterface: writes the key to the config file.
// Secrets and the serve.* keys (its own security) aren't set over HTTP.
func (a *API) SetConfig(_ context.Context, request apiv1.SetConfigRequestObject) (apiv1.SetConfigResponseObject, error) {
	key := request.Key
	if !config.Exists(key) {
		return nil, newAPIError(http.StatusNotFound, "not_found", "no config key %q", key)
	}
	if config.IsSecret(key) || strings.HasPrefix(key, "serve.") {
		return nil, newAPIError(http.StatusForbidden, CodeForbidden, "%s isn't set over HTTP (mangal config set, or the config file)", key)
	}
	body := deref(request.Body)
	if body.Value == nil {
		return nil, badRequest("value is needed")
	}
	if _, err := config.Parse(key, body.Value); err != nil {
		return nil, badRequest("%w", err)
	}
	if err := config.WriteKey(key, body.Value); err != nil {
		return nil, err
	}
	e, err := configEntry(key)
	if err != nil {
		return nil, err
	}
	a.Log.Infof("config %s = %v set through the API (applies: %s)", key, e.Value, e.Applies)
	return apiv1.SetConfig200JSONResponse(e), nil
}

// ListAccounts implements apiv1.StrictServerInterface.
func (a *API) ListAccounts(ctx context.Context, _ apiv1.ListAccountsRequestObject) (apiv1.ListAccountsResponseObject, error) {
	entries, err := client.AccountsCached(ctx)
	if err != nil {
		return nil, err
	}
	out := apiv1.ListAccounts200JSONResponse{}
	for _, e := range entries {
		acc := apiv1.Account{Service: e.Service, Status: e.Status, Checked: optTime(e.Checked), Expires: optTime(e.Expires)}
		for _, f := range []struct {
			v string
			p **string
		}{{e.Provider, &acc.Provider}, {e.User, &acc.User}, {e.URL, &acc.Url}} {
			if f.v != "" {
				v := f.v
				*f.p = &v
			}
		}
		if len(e.Details) != 0 {
			acc.Details = &e.Details
		}
		if e.Err != nil {
			msg := e.Err.Error()
			acc.Error = &msg
		}
		out = append(out, acc)
	}
	return out, nil
}

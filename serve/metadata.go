package serve

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// metadataProvider is client.MetadataProvider, swapped in tests.
var metadataProvider, defaultMetadataProvider = clientMetadataProvider, clientMetadataProvider

func clientMetadataProvider(source lmmeta.IDSource) (metadataSearcher, error) {
	return client.MetadataProvider(source)
}

// metadataSearcher is what the search uses of a lmmeta.ProviderWithCache.
type metadataSearcher interface {
	SearchByIDContext(ctx context.Context, id int) (lmmeta.Metadata, bool, error)
	SearchContext(ctx context.Context, query string) ([]lmmeta.Metadata, error)
	FindClosestContext(ctx context.Context, query lmmeta.Query) (lmmeta.Match, bool, error)
}

// ListMetadataSources implements apiv1.StrictServerInterface.
func (a *API) ListMetadataSources(context.Context, apiv1.ListMetadataSourcesRequestObject) (apiv1.ListMetadataSourcesResponseObject, error) {
	out := apiv1.ListMetadataSources200JSONResponse{}
	for _, src := range client.MetadataSources {
		s := apiv1.MetadataSource{Code: string(src.Code), Key: src.Key, Name: src.Name, Url: src.URL, Enabled: true}
		if _, err := metadataProvider(src.Source); err != nil {
			s.Enabled, s.Reason = false, new(err.Error())
		}
		out = append(out, s)
	}
	return out, nil
}

// metadataSites are the metadata sites' websites and icons (none: the website's
// /favicon.ico), by code and key.
var metadataSites = []struct{ code, key, website, icon string }{
	{string(lmmeta.IDCodeAnilist), "anilist", "https://anilist.co/", "https://anilist.co/img/icons/favicon-32x32.png"},
	{string(lmmeta.IDCodeKitsu), "kitsu", "https://kitsu.app/", ""},
	{string(lmmeta.IDCodeMyAnimeList), "myanimelist", "https://myanimelist.net/", "https://cdn.myanimelist.net/images/favicon.ico"},
	// TODO: only an SVG icon (refused), so a 404 for now
	{string(lmmeta.IDCodeMangaUpdates), "mangaupdates", "https://www.mangaupdates.com/", ""},
}

// GetMetadataIcon implements apiv1.StrictServerInterface.
func (a *API) GetMetadataIcon(ctx context.Context, request apiv1.GetMetadataIconRequestObject) (apiv1.GetMetadataIconResponseObject, error) {
	if a.Browser == nil {
		return nil, newAPIError(http.StatusServiceUnavailable, CodeUnavailable, "no image proxy here")
	}
	source := strings.ToLower(request.Source)
	for _, site := range metadataSites {
		if source != site.code && source != site.key {
			continue
		}
		data, contentType, err := a.Browser.Icon(ctx, site.icon, site.website)
		if err != nil {
			return nil, err
		}
		return apiv1.GetMetadataIcon200ImageResponse{Body: bytes.NewReader(data), ContentType: contentType, ContentLength: int64(len(data)),
			Headers: apiv1.GetMetadataIcon200ResponseHeaders{CacheControl: new(iconCache)}}, nil
	}
	return nil, fmt.Errorf("metadata source %q: %w", request.Source, core.ErrNotFound)
}

// metadataIDQuery is a search for an id: "123" or "id:123".
var metadataIDQuery = regexp.MustCompile(`^\s*(?:id:\s*)?(\d+)\s*$`)

// SearchMetadata implements apiv1.StrictServerInterface: the TUI's metadata search (the
// closest match on top, then the results scored).
func (a *API) SearchMetadata(ctx context.Context, request apiv1.SearchMetadataRequestObject) (apiv1.SearchMetadataResponseObject, error) {
	src, err := client.ParseMetadataSource(request.Source)
	if err != nil {
		return nil, badRequest("%w", err)
	}
	query := strings.TrimSpace(request.Params.Q)
	if query == "" {
		return nil, badRequest("q is needed")
	}
	p, err := metadataProvider(src.Source)
	if err != nil {
		return nil, unprocessable(err)
	}
	out := apiv1.SearchMetadata200JSONResponse{}
	if m := metadataIDQuery.FindStringSubmatch(query); m != nil {
		id, _ := strconv.Atoi(m[1])
		meta, found, err := p.SearchByIDContext(ctx, id)
		if err != nil {
			return nil, unprocessable(fmt.Errorf("get %s manga %d: %w", src.Name, id, err))
		}
		if found {
			out = append(out, toAPIMetadata(src, meta, 1))
		}
		return out, nil
	}
	q := lmmeta.Query{Titles: []string{query}}
	closest, found, err := p.FindClosestContext(ctx, q)
	if err != nil {
		return nil, unprocessable(fmt.Errorf("search %q on %s: %w", query, src.Name, err))
	}
	if found {
		out = append(out, toAPIMetadata(src, closest.Metadata, closest.Score))
	}
	results, err := p.SearchContext(ctx, query)
	if err != nil {
		return nil, unprocessable(fmt.Errorf("search %q on %s: %w", query, src.Name, err))
	}
	for _, m := range results {
		if found && m.ID() == closest.Metadata.ID() {
			continue
		}
		out = append(out, toAPIMetadata(src, m, lmmeta.Score(q, m)))
	}
	return out, nil
}

func toAPIMetadata(src client.MetadataSource, m lmmeta.Metadata, score float64) apiv1.MetadataResult {
	r := apiv1.MetadataResult{Source: string(src.Code), Id: m.ID().Raw, Title: m.Title(), Url: m.URL(), Score: score,
		Bad: score < lmmeta.GoodMatchScore}
	if r.Url == "" {
		r.Url = src.URL + r.Id
	}
	if y := m.StartDate().Year; y != 0 {
		r.Year = &y
	}
	for _, f := range []struct {
		v string
		p **string
	}{{m.Format(), &r.Format}, {strings.ToLower(string(m.Status())), &r.Status}, {m.Cover(), &r.Cover}} {
		if f.v != "" {
			v := f.v
			*f.p = &v
		}
	}
	if n := m.Chapters(); n != 0 {
		r.Chapters = &n
	}
	if alt := m.AlternateTitles(); len(alt) != 0 {
		r.AlternateTitles = &alt
	}
	if authors := m.Authors(); len(authors) != 0 {
		r.Authors = &authors
	}
	return r
}

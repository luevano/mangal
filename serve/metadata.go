package serve

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
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
			s.Enabled, s.Reason = false, ptr(err.Error())
		}
		out = append(out, s)
	}
	return out, nil
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

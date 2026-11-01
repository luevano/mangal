package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// MetadataSources are the codes of the metadata sources the server has enabled.
func (c *Client) MetadataSources(ctx context.Context) ([]string, error) {
	r, err := c.API.ListMetadataSourcesWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	sources, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	if err != nil {
		return nil, err
	}
	var codes []string
	for _, s := range sources {
		if s.Enabled {
			codes = append(codes, s.Code)
		}
	}
	return codes, nil
}

// Metadata searches the source through the server: its logins, its rate limits (browse).
func (c *Client) Metadata(src client.MetadataSource) *MetadataSearcher {
	return &MetadataSearcher{c: c, src: src}
}

// MetadataSearcher is a client.MetadataSearcher over the server's searchMetadata.
type MetadataSearcher struct {
	c   *Client
	src client.MetadataSource
}

// Hits are the server's answer as it is: the closest match first, scored there.
func (s *MetadataSearcher) Hits(ctx context.Context, query string) ([]client.MetadataHit, error) {
	r, err := s.c.API.SearchMetadataWithResponse(ctx, s.src.Key, &apiv1.SearchMetadataParams{Q: query})
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	results, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	if err != nil {
		return nil, err
	}
	hits := make([]client.MetadataHit, len(results))
	for i, res := range results {
		hits[i] = client.MetadataHit{Meta: metadataResult{r: res, src: s.src}, Score: res.Score, Bad: res.Bad}
	}
	return hits, nil
}

func (s *MetadataSearcher) SearchByIDContext(ctx context.Context, id int) (lmmeta.Metadata, bool, error) {
	hits, err := s.Hits(ctx, fmt.Sprintf("id:%d", id))
	if err != nil || len(hits) == 0 {
		return nil, false, err
	}
	return hits[0].Meta, true, nil
}

func (s *MetadataSearcher) SearchContext(ctx context.Context, query string) ([]lmmeta.Metadata, error) {
	hits, err := s.Hits(ctx, query)
	if err != nil {
		return nil, err
	}
	metas := make([]lmmeta.Metadata, len(hits))
	for i, h := range hits {
		metas[i] = h.Meta
	}
	return metas, nil
}

func (s *MetadataSearcher) FindClosestContext(ctx context.Context, q lmmeta.Query) (lmmeta.Match, bool, error) {
	hits, err := s.Hits(ctx, strings.Join(q.Titles, " "))
	if err != nil || len(hits) == 0 {
		return lmmeta.Match{}, false, err
	}
	return lmmeta.Match{Metadata: hits[0].Meta, Score: hits[0].Score}, true, nil
}

// metadataResult is what the API says of a manga: the rest of a Metadata is empty (Partial),
// only the id goes back to the server.
type metadataResult struct {
	r   apiv1.MetadataResult
	src client.MetadataSource
}

var _ lmmeta.Partial = metadataResult{}

func (m metadataResult) Partial() bool { return true }

// MarshalJSON is the API's result (mangal inline json on a server).
func (m metadataResult) MarshalJSON() ([]byte, error) { return json.Marshal(m.r) }

func (m metadataResult) String() string {
	if m.r.Year != nil {
		return fmt.Sprintf("%s (%d)", m.r.Title, *m.r.Year)
	}
	return m.r.Title
}
func (m metadataResult) Title() string             { return m.r.Title }
func (m metadataResult) AlternateTitles() []string { return deref(m.r.AlternateTitles) }
func (m metadataResult) Score() float32            { return 0 }
func (m metadataResult) Description() string       { return "" }
func (m metadataResult) Cover() string             { return deref(m.r.Cover) }
func (m metadataResult) Banner() string            { return "" }
func (m metadataResult) Tags() []string            { return nil }
func (m metadataResult) Genres() []string          { return nil }
func (m metadataResult) Characters() []string      { return nil }
func (m metadataResult) Authors() []string         { return deref(m.r.Authors) }
func (m metadataResult) Artists() []string         { return nil }
func (m metadataResult) Translators() []string     { return nil }
func (m metadataResult) Letterers() []string       { return nil }
func (m metadataResult) StartDate() lmmeta.Date {
	return lmmeta.Date{Year: deref(m.r.Year)}
}
func (m metadataResult) EndDate() lmmeta.Date { return lmmeta.Date{} }
func (m metadataResult) Publisher() string    { return "" }
func (m metadataResult) Status() lmmeta.Status {
	return lmmeta.Status(strings.ToUpper(deref(m.r.Status)))
}
func (m metadataResult) Format() string        { return deref(m.r.Format) }
func (m metadataResult) Country() string       { return "" }
func (m metadataResult) Chapters() int         { return deref(m.r.Chapters) }
func (m metadataResult) Notes() string         { return "" }
func (m metadataResult) URL() string           { return m.r.Url }
func (m metadataResult) ExtraIDs() []lmmeta.ID { return nil }
func (m metadataResult) ID() lmmeta.ID {
	return lmmeta.ID{Raw: m.r.Id, Source: m.src.Source, Code: m.src.Code}
}

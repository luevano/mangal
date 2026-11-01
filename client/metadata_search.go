package client

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	"github.com/luevano/libmangal/metadata"
)

// MetadataSearcher is what SearchMetadataSource uses of a metadata.ProviderWithCache.
type MetadataSearcher interface {
	SearchByIDContext(ctx context.Context, id int) (metadata.Metadata, bool, error)
	SearchContext(ctx context.Context, query string) ([]metadata.Metadata, error)
	FindClosestContext(ctx context.Context, query metadata.Query) (metadata.Match, bool, error)
}

// MetadataHit is a result of SearchMetadataSource. Bad is a match too unlike the search to
// be used automatically, it can still be picked.
type MetadataHit struct {
	Meta  metadata.Metadata
	Score float64
	Bad   bool
}

// metadataIDQuery is a search for an id: "123" or "id:123".
var metadataIDQuery = regexp.MustCompile(`^\s*(?:id:\s*)?(\d+)\s*$`)

// SearchMetadataSource is a source's part of an interactive metadata search: the manga
// with the id for a number or "id:<n>", else the closest match on top, then the results scored.
func SearchMetadataSource(ctx context.Context, src MetadataSource, p MetadataSearcher, query string) ([]MetadataHit, error) {
	// a server's search has already done this
	if h, ok := p.(interface {
		Hits(ctx context.Context, query string) ([]MetadataHit, error)
	}); ok {
		return h.Hits(ctx, query)
	}
	if m := metadataIDQuery.FindStringSubmatch(query); m != nil {
		id, _ := strconv.Atoi(m[1])
		meta, found, err := p.SearchByIDContext(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("get %s manga %d: %w", src.Name, id, err)
		}
		if !found {
			return nil, nil
		}
		return []MetadataHit{{Meta: meta, Score: 1}}, nil
	}

	q := metadata.Query{Titles: []string{query}}
	var hits []MetadataHit
	closest, found, err := p.FindClosestContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("search %q on %s: %w", query, src.Name, err)
	}
	if found {
		hits = append(hits, MetadataHit{Meta: closest.Metadata, Score: closest.Score, Bad: closest.Bad()})
	}
	results, err := p.SearchContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("search %q on %s: %w", query, src.Name, err)
	}
	for _, m := range results {
		if found && m.ID() == closest.Metadata.ID() {
			continue
		}
		score := metadata.Score(q, m)
		hits = append(hits, MetadataHit{Meta: m, Score: score, Bad: score < metadata.GoodMatchScore})
	}
	return hits, nil
}

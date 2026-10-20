package core

import (
	"context"
	"fmt"
	"sync"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client"
)

// SearchConcurrency is how many providers a multi-provider search asks at once.
const SearchConcurrency = 4

// SearchReq searches Query on each of Providers (ids), at most Concurrency at once
// (SearchConcurrency when 0).
type SearchReq struct {
	Providers   []string
	Query       string
	Concurrency int
}

// SearchResult is one provider's answer: its mangas, or the error. Provider is the id
// searched, Info the provider's (empty when it couldn't be loaded).
type SearchResult struct {
	Provider string
	Info     libmangal.ProviderInfo
	Mangas   []mangadata.Manga
	Err      error
}

// Search searches the query on the providers concurrently: each provider's result comes
// on the channel as soon as it answers (its own throttle applies), the channel is closed
// once every one did. A cancelled ctx ends the searches still running (their results
// carry the error); the channel holds every result, so a reader may stop early.
func (c *Core) Search(ctx context.Context, req SearchReq) <-chan SearchResult {
	results := make(chan SearchResult, len(req.Providers))
	limit := req.Concurrency
	if limit <= 0 {
		limit = SearchConcurrency
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for _, p := range req.Providers {
		wg.Go(func() {
			r := SearchResult{Provider: p}
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
				r.Info, r.Mangas, r.Err = c.env.SearchProvider(ctx, p, req.Query)
			case <-ctx.Done():
				r.Err = ctx.Err()
			}
			results <- r
		})
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	return results
}

// searchProvider searches the query on the provider with the id.
func searchProvider(ctx context.Context, provider, query string) (libmangal.ProviderInfo, []mangadata.Manga, error) {
	cl, _, release, err := client.HoldByID(ctx, provider)
	if err != nil {
		return libmangal.ProviderInfo{}, nil, err
	}
	defer release()
	mangas, err := cl.SearchMangasContext(ctx, query)
	if err != nil {
		return cl.Info(), nil, fmt.Errorf("search %q: %w", query, err)
	}
	return cl.Info(), mangas, nil
}

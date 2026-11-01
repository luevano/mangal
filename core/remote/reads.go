package remote

import (
	"context"
	"fmt"

	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/web/apiv1"
)

func deref[T any](p *T) T {
	var v T
	if p != nil {
		v = *p
	}
	return v
}

// Ops are the server's journal, newest first, at most limit (0 for all).
func (c *Client) Ops(ctx context.Context, limit int) ([]storage.Op, error) {
	r, err := c.API.ListOpsWithResponse(ctx, &apiv1.ListOpsParams{Limit: opt(limit)})
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	list, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	if err != nil {
		return nil, err
	}
	ops := make([]storage.Op, len(list))
	for i, op := range list {
		ops[i] = storage.Op{ID: op.Id, Kind: op.Kind, Summary: op.Summary, Started: op.Started, Steps: op.Steps,
			Finished: deref(op.Finished), Undone: deref(op.Undone), Error: deref(op.Error), Snapshot: deref(op.Snapshot),
			Interfered: deref(op.Interfered), Origin: deref(op.Origin)}
	}
	return ops, nil
}

// History is the token's user's read chapters on the server (of the provider, of its
// manga when mangaID is set), last read first.
func (c *Client) History(ctx context.Context, provider, mangaID string) ([]storage.Read, error) {
	r, err := c.API.ListHistoryWithResponse(ctx, &apiv1.ListHistoryParams{Provider: opt(provider), MangaId: opt(mangaID)})
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	list, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	if err != nil {
		return nil, err
	}
	reads := make([]storage.Read, len(list.Reads))
	for i, rd := range list.Reads {
		reads[i] = storage.Read{Provider: rd.Provider, MangaID: rd.MangaId, MangaTitle: rd.MangaTitle, Volume: rd.Volume,
			Chapter: rd.Chapter, ChapterTitle: rd.ChapterTitle, ChapterURL: deref(rd.ChapterUrl), LibraryManga: deref(rd.LibraryManga),
			Count: rd.Count, First: rd.First, Last: rd.Last}
	}
	return reads, nil
}

// Services are the core services over the server. What its API doesn't have yet answers
// an error matching errors.ErrUnsupported; the jobs are Jobs.
func (c *Client) Services() core.Services {
	return core.Services{Remote: c.URL, Changes: c.Changes(), Library: remoteLibrary{c}, History: history{c},
		Bindings: remoteBindings{c}, Journal: c, Health: remoteHealth{c}, Logs: remoteLogs{c}, Providers: remoteProviders{c},
		Config: remoteConfig{c}, Accounts: remoteAccounts{c}, Users: remoteUsers{c},
		Watches: func(context.Context) (watch.Manager, error) { return c.Watches(), nil }}
}

type history struct{ c *Client }

func (h history) List(ctx context.Context, provider, mangaID string) ([]storage.Read, error) {
	return h.c.History(ctx, provider, mangaID)
}

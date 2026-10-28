package core

import (
	"context"

	"github.com/luevano/mangal/v5/storage"
)

// History is the read history of the ctx's user (HistoryUser); clearing it is a Change
// (HistoryClearReq).
type History interface {
	// List is the read chapters (of the provider, or its manga), last read first.
	List(ctx context.Context, provider, mangaID string) ([]storage.Read, error)
}

type localHistory struct {
	core *Core
}

func (h *localHistory) List(ctx context.Context, provider, mangaID string) ([]storage.Read, error) {
	return h.core.env.Storage.History().Of(HistoryUser(ctx)).List(provider, mangaID)
}

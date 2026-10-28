package core

import (
	"context"

	"github.com/luevano/mangal/v5/storage"
)

// Bindings are the providers' mangas' metadata bindings; binding and unbinding are
// Changes (BindReq, UnbindReq).
type Bindings interface {
	// List is the bindings of a provider and manga (empty for any).
	List(ctx context.Context, provider, mangaID string) ([]storage.Binding, error)
	// OfManga is the bindings of the library manga's sources, then its own ids.
	OfManga(ctx context.Context, id int64) ([]storage.Binding, error)
}

type localBindings struct {
	core *Core
}

func (b *localBindings) List(_ context.Context, provider, mangaID string) ([]storage.Binding, error) {
	return b.core.env.Storage.Bindings().List(provider, mangaID)
}

func (b *localBindings) OfManga(_ context.Context, id int64) ([]storage.Binding, error) {
	return b.core.env.Storage.Bindings().OfLibraryManga(id)
}

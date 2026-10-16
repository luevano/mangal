package core

import (
	"context"
	"fmt"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/provider/manager"
)

// Providers are the installed providers.
type Providers interface {
	List(ctx context.Context) ([]libmangal.ProviderInfo, error)
	// Info is the provider with the id (an old id too), a *NotFoundError when there's none.
	Info(ctx context.Context, id string) (libmangal.ProviderInfo, error)
}

// NewProviders are the providers of loaders (manager.Loaders when nil), without the rest
// of the services (no storage).
func NewProviders(loaders func() ([]libmangal.ProviderLoader, error)) Providers {
	if loaders == nil {
		loaders = manager.Loaders
	}
	return &localProviders{loaders: loaders}
}

type localProviders struct {
	loaders func() ([]libmangal.ProviderLoader, error)
}

func (p *localProviders) List(context.Context) ([]libmangal.ProviderInfo, error) {
	loaders, err := p.loaders()
	if err != nil {
		return nil, err
	}
	infos := make([]libmangal.ProviderInfo, len(loaders))
	for i, l := range loaders {
		infos[i] = l.Info()
	}
	return infos, nil
}

func (p *localProviders) Info(_ context.Context, id string) (libmangal.ProviderInfo, error) {
	loaders, err := p.loaders()
	if err != nil {
		return libmangal.ProviderInfo{}, err
	}
	l, _, ok := libmangal.FindLoader(loaders, id)
	if !ok {
		return libmangal.ProviderInfo{}, &NotFoundError{What: fmt.Sprintf("no provider %q", id)}
	}
	return l.Info(), nil
}

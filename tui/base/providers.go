package base

import (
	"context"
	"fmt"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/provider/manager"
)

// ProviderLoaders are the providers to browse: this machine's, the server's on a remote
// TUI (problems are the local ones that can't be loaded).
func ProviderLoaders(ctx context.Context) (loaders []libmangal.ProviderLoader, problems []error, err error) {
	if r := CurrentRemote; r != nil {
		loaders, err = r.Loaders(ctx)
		return loaders, nil, err
	}
	return manager.Load()
}

// HoldProvider is the client of the provider with the id (an old id too), held until
// release is called: this machine's, the server's on a remote TUI.
func HoldProvider(ctx context.Context, id string) (*libmangal.Client, func(), error) {
	if r := CurrentRemote; r != nil {
		loaders, err := r.Loaders(ctx)
		if err != nil {
			return nil, nil, err
		}
		loader, _, ok := libmangal.FindLoader(loaders, id)
		if !ok {
			return nil, nil, &core.NotFoundError{What: fmt.Sprintf("no provider %q on %s", id, r.Host())}
		}
		return client.Hold(ctx, loader)
	}
	c, _, release, err := client.HoldByID(ctx, id)
	return c, release, err
}

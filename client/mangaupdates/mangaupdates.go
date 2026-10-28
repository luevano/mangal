// Package mangaupdates is the MangaUpdates metadata provider shared by every client
// (read-only: MangaUpdates has no login or progress here).
package mangaupdates

import (
	"fmt"
	"sync"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/libmangal/metadata/mangaupdates"
	mlog "github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/provider/loader"
	"github.com/luevano/mangal/v5/util/cache"
)

// MangaUpdates is the MangaUpdates metadata provider shared by every client.
var MangaUpdates = sync.OnceValues(func() (*metadata.ProviderWithCache, error) {
	opts := mangaupdates.DefaultOptions()
	opts.HTTPClient = loader.HTTPClient()
	mu, err := mangaupdates.NewMangaUpdates(opts)
	if err != nil {
		return nil, fmt.Errorf("create mangaupdates: %w", err)
	}
	cacheOpts := metadata.DefaultProviderWithCacheOptions()
	cacheOpts.Provider = mu
	cacheOpts.CacheStore = cache.CacheStore
	p, err := metadata.NewProviderWithCache(cacheOpts)
	if err != nil {
		return nil, fmt.Errorf("create mangaupdates: %w", err)
	}
	p.Logger().SetOnLog(mlog.ProviderHook("mangaupdates"))
	return p, nil
})

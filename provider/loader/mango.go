package loader

import (
	"fmt"
	"net/http"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/util/cache"
	mango "github.com/luevano/mangoprovider"
	"github.com/luevano/mangoprovider/apis"
	"github.com/luevano/mangoprovider/scrapers"
)

func MangoLoaders(filters ...FilterOption) ([]libmangal.ProviderLoader, error) {
	o := mangoOptions(filters...)

	var loaders []libmangal.ProviderLoader
	loaders = append(loaders, apis.Loaders(o)...)
	loaders = append(loaders, scrapers.Loaders(o)...)

	for i, loader := range loaders {
		if loader == nil {
			return nil, fmt.Errorf("mangoprovider loader %d is nil", i)
		}
	}

	return loaders, nil
}

// mangoOptions are mangoprovider's default options with the config's.
func mangoOptions(filters ...FilterOption) mango.Options {
	o := mango.DefaultOptions()

	c := commonOptions(filters...)
	o.HTTPClient = ProviderHTTPClient()
	o.UserAgent = c.UserAgent
	o.CacheStore = cache.CacheStore
	o.Parallelism = config.Providers.Parallelism.Get()
	o.Filter = c.Filter
	o.Headless.UseFlaresolverr = config.Providers.Headless.UseFlaresolverr.Get()
	o.Headless.FlaresolverrURL = config.Providers.Headless.FlaresolverrURL.Get()
	// no timeout, each solve has its own; not guarded, the user chose it
	o.Headless.FlaresolverrClient = &http.Client{Transport: DebugTransport(nil)}
	o.Debug = debugLog()
	o.Mirrors = config.Providers.Mirrors.Get()
	// the providers' settings (MangaDex's groups, MangaPlus' quality, ...): flag,
	// environment, database, the provider's default
	o.Settings = config.ProviderValues
	o.MangaPlus.Secret = mangaPlusSecret()
	o.MangaPlus.FreeSecret = mangaPlusFreeSecret()
	o.MangaPlus.OnRegister = saveMangaPlusFreeSecret
	return o
}

// mangaPlusSecret is the saved secret (mangal accounts), empty if none: then MangaPlus
// reads its secret_file setting, or uses the free tier.
func mangaPlusSecret() string {
	secret, err := cache.MangaPlusSecret()
	if err != nil {
		log.Error("MangaPlus secret: %s", err)
	}
	return secret
}

// mangaPlusFreeSecret is the saved free device, empty if none: then MangaPlus uses its
// free_secret setting, or registers one (saveMangaPlusFreeSecret).
func mangaPlusFreeSecret() string {
	secret, err := cache.MangaPlusFreeSecret()
	if err != nil {
		log.Error("MangaPlus free device: %s", err)
	}
	return secret
}

// saveMangaPlusFreeSecret saves a newly registered free device, so it's reused instead of registering a new one.
func saveMangaPlusFreeSecret(secret string) {
	if err := cache.SetMangaPlusFreeSecret(secret); err != nil {
		msg := fmt.Sprintf("couldn't save the MangaPlus free device secret: %s", err)
		log.Warn("%s", msg)
	}
}

package loader

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/luevano/mangoplus"
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
	o.MangaDex.DataSaver = config.Providers.MangaDex.DataSaver.Get()
	o.MangaDex.ExcludeGroups = config.Providers.MangaDex.ExcludeGroups.Get()
	o.MangaDex.PreferGroups = config.Providers.MangaDex.PreferGroups.Get()
	o.MangaPlus.Quality = config.Providers.MangaPlus.Quality.Get()
	o.MangaPlus.OSVersion = config.Providers.MangaPlus.OSVersion.Get()
	o.MangaPlus.AppVersion = config.Providers.MangaPlus.AppVersion.Get()
	// AndroidID is the only config defaulted to empty string,
	// mangoprovider generates a random one when using default options
	if androidID := config.Providers.MangaPlus.AndroidID.Get(); androidID != "" {
		o.MangaPlus.AndroidID = androidID
	}
	o.MangaPlus.API = mangoplus.API(config.Providers.MangaPlus.API.Get())
	o.MangaPlus.Viewer = mangoplus.Viewer(config.Providers.MangaPlus.Viewer.Get())
	// already validated by the config
	o.MangaPlus.RequestInterval, _ = time.ParseDuration(config.Providers.MangaPlus.RequestInterval.Get())
	o.MangaPlus.Secret = mangaPlusSecret()
	o.MangaPlus.FreeSecret = mangaPlusFreeSecret()
	o.MangaPlus.OnRegister = saveMangaPlusFreeSecret
	return o
}

// mangaPlusSecret is the saved secret (mangal accounts), or the one in the configured file,
// empty (free tier) if none.
func mangaPlusSecret() string {
	if secret, err := cache.MangaPlusSecret(); err != nil {
		log.Error("MangaPlus secret: %s", err)
	} else if secret != "" {
		return secret
	}

	path := config.Providers.MangaPlus.SecretFile.Get()
	if path == "" {
		return ""
	}

	data, err := os.ReadFile(path)
	if err != nil {
		msg := fmt.Sprintf("couldn't read MangaPlus secret file %q, using free tier: %s", path, err)
		if errors.Is(err, fs.ErrNotExist) {
			msg = fmt.Sprintf("MangaPlus secret file %q doesn't exist, using free tier", path)
		}
		log.Warn("%s", msg)
		return ""
	}
	return strings.TrimSpace(string(data))
}

// mangaPlusFreeSecret is the saved free device, imported once from the config (where
// mangal 5.13 and older saved it).
func mangaPlusFreeSecret() string {
	secret, err := cache.MangaPlusFreeSecret()
	if err != nil {
		log.Error("MangaPlus free device: %s", err)
	}
	if secret != "" {
		return secret
	}
	if secret = config.Providers.MangaPlus.FreeSecret.Get(); secret != "" {
		saveMangaPlusFreeSecret(secret)
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

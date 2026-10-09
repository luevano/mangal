package loader

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/config"
	"github.com/luevano/mangal/log"
	"github.com/luevano/mangal/util/cache"
	"github.com/luevano/mangoplus"
	mango "github.com/luevano/mangoprovider"
	"github.com/luevano/mangoprovider/apis"
	"github.com/luevano/mangoprovider/scrapers"
)

func MangoLoaders() ([]libmangal.ProviderLoader, error) {
	// Generates overall default options then overriding as necessary
	o := mango.DefaultOptions()

	o.HTTPClient.Timeout = time.Minute
	o.UserAgent = config.Download.UserAgent.Get()
	o.CacheStore = cache.CacheStore
	o.Parallelism = config.Providers.Parallelism.Get()
	o.Filter.NSFW = config.Providers.Filter.NSFW.Get()
	o.Filter.Language = config.Providers.Filter.Language.Get()
	o.Filter.AvoidDuplicateChapters = config.Providers.Filter.AvoidDuplicateChapters.Get()
	o.Filter.ShowUnavailableChapters = config.Providers.Filter.ShowUnavailableChapters.Get()
	o.Headless.UseFlaresolverr = config.Providers.Headless.UseFlaresolverr.Get()
	o.Headless.FlaresolverrURL = config.Providers.Headless.FlaresolverrURL.Get()
	o.MangaDex.DataSaver = config.Providers.MangaDex.DataSaver.Get()
	o.MangaPlus.Quality = config.Providers.MangaPlus.Quality.Get()
	o.MangaPlus.OSVersion = config.Providers.MangaPlus.OSVersion.Get()
	o.MangaPlus.AppVersion = config.Providers.MangaPlus.AppVersion.Get()
	// AndroidID is the only config defaulted to empty string,
	// mangoprovider generates a random one when using default options
	if androidID := config.Providers.MangaPlus.AndroidID.Get(); androidID != "" {
		o.MangaPlus.AndroidID = androidID
	}
	o.MangaPlus.Viewer = mangoplus.Viewer(config.Providers.MangaPlus.Viewer.Get())
	// already validated by the config
	o.MangaPlus.RequestInterval, _ = time.ParseDuration(config.Providers.MangaPlus.RequestInterval.Get())
	o.MangaPlus.Secret = mangaPlusSecret()
	o.MangaPlus.FreeSecret = config.Providers.MangaPlus.FreeSecret.Get()
	o.MangaPlus.OnRegister = saveMangaPlusFreeSecret

	var loaders []libmangal.ProviderLoader
	loaders = append(loaders, apis.Loaders(o)...)
	loaders = append(loaders, scrapers.Loaders(o)...)

	for _, loader := range loaders {
		if loader == nil {
			// TODO: need to provide more info
			return nil, fmt.Errorf("failed while loading providers")
		}
	}

	return loaders, nil
}

// mangaPlusSecret reads the secret from the configured file, empty (free tier) if not set or missing.
func mangaPlusSecret() string {
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
		log.L.Warn().Msg(msg)
		log.Log("%s", msg)
		return ""
	}
	return strings.TrimSpace(string(data))
}

// saveMangaPlusFreeSecret saves a newly registered free device, so it's reused instead of registering a new one.
func saveMangaPlusFreeSecret(secret string) {
	// mangal only creates its config file when asked (config write/set), don't create it here
	if _, err := os.Stat(config.Path); err != nil {
		return
	}
	// only this key, config.Write would also save the flags of this run
	if err := config.WriteKey(config.Providers.MangaPlus.FreeSecret.Key, secret); err != nil {
		msg := fmt.Sprintf("couldn't save the MangaPlus free device secret: %s", err)
		log.L.Warn().Msg(msg)
		log.Log("%s", msg)
	}
}

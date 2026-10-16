package loader

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/luevano/libmangal/netutil"
	"github.com/luevano/luaprovider"
	sdk "github.com/luevano/luaprovider/lib"
	luaoptions "github.com/luevano/luaprovider/lib/options"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/luevano/mangal/v5/util/netguard"
	"github.com/luevano/mangoplus"
)

// HTTPTimeout is the timeout of every HTTP request mangal and the providers make.
const HTTPTimeout = time.Minute

// HTTPClient is a new client with HTTPTimeout, its requests logged with --debug.
func HTTPClient() *http.Client {
	return &http.Client{Timeout: HTTPTimeout, Transport: DebugTransport(nil)}
}

// Guard is the private address guard from the config (providers.allow_private_addresses,
// providers.private_allowlist), the hosts of providers.mirrors allowed.
func Guard() *netguard.Guard {
	allow := slices.Clone(config.Providers.Private.Allowlist.Get())
	for _, mirror := range config.Providers.Mirrors.Get() {
		if u, err := url.Parse(mirror); err == nil && u.Hostname() != "" {
			allow = append(allow, u.Hostname())
		}
	}
	options := netguard.Options{AllowPrivate: config.Providers.Private.Allow.Get(), Allow: allow}
	guard, err := netguard.New(options)
	if err != nil {
		// the config validates the allowlist, so only the mirrors are left
		log.Error("private address guard: %s", err)
		options.Allow = nil
		guard, _ = netguard.New(options)
	}
	return guard
}

// ProviderHTTPClient is HTTPClient through Guard: the client of the providers and of what
// fetches their urls.
func ProviderHTTPClient() *http.Client {
	return Guard().Client(HTTPClient())
}

// DebugTransport is next (nil: http.DefaultTransport) logging every request at debug level
// (--debug), secrets in the url redacted (MangaPlus' unless MANGOPLUS_UNSAFE_SHOW_SECRET=1),
// headers never logged. Without --debug it's next as is.
func DebugTransport(next http.RoundTripper) http.RoundTripper {
	if !log.Enabled(log.LevelDebug) {
		return next
	}
	return netutil.NewLoggingTransport(next, func(format string, a ...any) {
		log.Debug("http: %s", fmt.Sprintf(format, a...))
	}, redactURL)
}

func redactURL(u *url.URL) string {
	return mangoplus.RedactURL(netutil.RedactURL(u))
}

// common are the options every kind of provider takes from the config.
type common struct {
	UserAgent string
	Filter    luaoptions.Filter
}

func commonOptions() common {
	return common{
		UserAgent: config.Download.UserAgent.Get(),
		Filter: luaoptions.Filter{
			NSFW:                    config.Providers.Filter.NSFW.Get(),
			Language:                config.Providers.Filter.Language.Get(),
			AvoidDuplicateChapters:  config.Providers.Filter.AvoidDuplicateChapters.Get(),
			ShowUnavailableChapters: config.Providers.Filter.ShowUnavailableChapters.Get(),
		},
	}
}

// LuaOptions are the options of a Lua provider in dir, from the config.
func LuaOptions(dir string) luaprovider.Options {
	c := commonOptions()
	return luaprovider.Options{
		HTTPClient:   ProviderHTTPClient(),
		UserAgent:    c.UserAgent,
		CacheStore:   cache.CacheStore,
		PackagePaths: []string{dir},
		Filter:       c.Filter,
	}
}

// SDKOptions are the options of the Lua sdk library outside a provider (mangal script),
// from the config, cached under name. Close the returned closer when done: it closes what
// the scripts opened (e.g. headless browsers).
func SDKOptions(name string) (*sdk.Options, io.Closer, error) {
	store, err := cache.CacheStore(name, name)
	if err != nil {
		return nil, nil, err
	}
	c := commonOptions()
	t := &tracker{}
	return &sdk.Options{
		HTTPClient: ProviderHTTPClient(),
		CacheStore: store,
		UserAgent:  c.UserAgent,
		Log:        log.ProviderHook(name),
		Track:      t.track,
		Filter:     c.Filter,
	}, t, nil
}

// tracker closes the resources the sdk opened.
type tracker struct {
	mu      sync.Mutex
	closers []io.Closer
}

func (t *tracker) track(closer io.Closer) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closers = append(t.closers, closer)
}

func (t *tracker) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	var errs []error
	for _, closer := range t.closers {
		errs = append(errs, closer.Close())
	}
	t.closers = nil
	return errors.Join(errs...)
}

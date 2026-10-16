package client

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/libmangal/progress"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/client/kitsu"
	"github.com/luevano/mangal/v5/client/myanimelist"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/path"
	providerloader "github.com/luevano/mangal/v5/provider/loader"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/luevano/mangal/v5/template"
	"github.com/luevano/mangal/v5/util/afs"
	"github.com/luevano/mangal/v5/util/cache"
)

// entry is a client of the registry, done is closed once it's created (or failed).
type entry struct {
	done   chan struct{}
	client *libmangal.Client
	err    error
	// auth is the logins/secrets fingerprint the client was made with
	auth string

	// the fields below are guarded by clientsMu

	// refs are the holders (Hold) using it
	refs int
	// retired is out of the registry (CloseAll, ReleaseIfAuthChanged): closed once the
	// last holder releases it
	retired bool
	closed  bool
}

var (
	clients   = map[string]*entry{}
	clientsMu sync.Mutex
	// lingering are the retired entries still held
	lingering = map[*entry]struct{}{}

	// create and authFingerprint are swapped in tests
	create          = newClient
	authFingerprint = cache.AuthFingerprint
)

// ErrProviderNotFound is a provider id that isn't installed (nor an old id of one).
var ErrProviderNotFound = errors.New("provider not installed")

// Get returns the provider's client if it's already created, nil otherwise.
func Get(loader libmangal.ProviderLoader) *libmangal.Client {
	clientsMu.Lock()
	e, ok := clients[loader.Info().ID]
	clientsMu.Unlock()
	if !ok {
		return nil
	}
	select {
	case <-e.done:
		return e.client
	default:
		return nil
	}
}

// Acquire returns the provider's client, creating it if there's none yet. There's one
// client per provider: callers share it and don't close it. It isn't held: CloseAll and
// ReleaseIfAuthChanged may close it while it's used, work that can overlap them (a
// check, a request) uses Hold.
func Acquire(ctx context.Context, loader libmangal.ProviderLoader) (*libmangal.Client, error) {
	e, err := acquire(ctx, loader)
	if err != nil {
		return nil, err
	}
	return e.client, nil
}

// Hold is Acquire keeping the client open until release is called (once the work with
// it is done): CloseAll and ReleaseIfAuthChanged drop it from the registry meanwhile and
// close it on the last release.
func Hold(ctx context.Context, loader libmangal.ProviderLoader) (_ *libmangal.Client, release func(), _ error) {
	for {
		e, err := acquire(ctx, loader)
		if err != nil {
			return nil, nil, err
		}
		clientsMu.Lock()
		if e.retired {
			// closed (or closing) since: take the next one
			clientsMu.Unlock()
			continue
		}
		e.refs++
		clientsMu.Unlock()
		return e.client, sync.OnceFunc(func() { unref(e) }), nil
	}
}

func unref(e *entry) {
	clientsMu.Lock()
	e.refs--
	closeNow := e.retired && e.refs == 0 && !e.closed
	if e.refs == 0 {
		delete(lingering, e)
	}
	if closeNow {
		e.closed = true
	}
	clientsMu.Unlock()
	if closeNow {
		if err := e.client.Close(); err != nil {
			log.Warn("close %s: %s", e.client, err)
		}
	}
}

func acquire(ctx context.Context, loader libmangal.ProviderLoader) (*entry, error) {
	id := loader.Info().ID
	for {
		clientsMu.Lock()
		e, ok := clients[id]
		if !ok {
			e = &entry{done: make(chan struct{})}
			clients[id] = e
		}
		clientsMu.Unlock()

		if !ok {
			// created outside the lock, the others wait on done
			e.auth, _ = authFingerprint()
			// loading may take a while (a FlareSolverr probe, rod's launch)
			progress.Report(ctx, progress.Status{Source: id, Message: "loading " + loader.Info().Name})
			e.client, e.err = create(ctx, loader)
			if e.err != nil {
				clientsMu.Lock()
				if clients[id] == e {
					delete(clients, id)
				}
				clientsMu.Unlock()
			}
			close(e.done)
			if e.err != nil {
				return nil, e.err
			}
			return e, nil
		}

		select {
		case <-e.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if e.err == nil {
			return e, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// the one creating it was cancelled, try again with this ctx
		if !errors.Is(e.err, context.Canceled) && !errors.Is(e.err, context.DeadlineExceeded) {
			return nil, e.err
		}
	}
}

// AcquireByID is Acquire for the provider with the id, which may be an alias (old id) of
// it. The warnings tell about the alias and data still under old ids.
func AcquireByID(ctx context.Context, id string) (*libmangal.Client, []string, error) {
	loader, warnings, err := loaderByID(id)
	if err != nil {
		return nil, warnings, err
	}
	client, err := Acquire(ctx, loader)
	if err != nil {
		return nil, warnings, err
	}
	return client, warnings, nil
}

// HoldByID is Hold for the provider with the id, as AcquireByID.
func HoldByID(ctx context.Context, id string) (_ *libmangal.Client, warnings []string, release func(), _ error) {
	loader, warnings, err := loaderByID(id)
	if err != nil {
		return nil, warnings, nil, err
	}
	client, release, err := Hold(ctx, loader)
	if err != nil {
		return nil, warnings, nil, err
	}
	return client, warnings, release, nil
}

// CurrentProviderID is the current id of the installed provider with the id, an old one
// too (libmangal.ProviderInfo.History); the id as is when none has it.
func CurrentProviderID(id string) string {
	loaders, _, err := manager.Load()
	if err != nil {
		return id
	}
	if loader, _, ok := libmangal.FindLoader(loaders, id); ok {
		return loader.Info().ID
	}
	return id
}

func loaderByID(id string) (libmangal.ProviderLoader, []string, error) {
	loaders, problems, err := manager.Load()
	if err != nil {
		return nil, nil, err
	}
	var warnings []string
	for _, p := range problems {
		warnings = append(warnings, fmt.Sprintf("skipped %s", p))
	}

	loader, alias, ok := libmangal.FindLoader(loaders, id)
	if !ok {
		return nil, warnings, fmt.Errorf("provider with id %q: %w", id, ErrProviderNotFound)
	}
	if alias != nil {
		info := loader.Info()
		warnings = append(warnings, fmt.Sprintf("provider %q is now %q (%s), use that id instead", id, info.ID, info.Name))
	}
	if notice, _, err := cache.OldIDsNotice(loaders); err == nil && notice != "" {
		warnings = append(warnings, notice)
	}
	for _, w := range warnings {
		log.Warn("%s", w)
	}
	return loader, warnings, nil
}

// retire takes the ready entry out of the registry (clientsMu held): true when it's
// idle and the caller closes it now, else its last holder does.
func retire(id string, e *entry) bool {
	if clients[id] == e {
		delete(clients, id)
	}
	e.retired = true
	if e.closed {
		return false
	}
	if e.refs > 0 {
		lingering[e] = struct{}{}
		return false
	}
	e.closed = true
	return true
}

// Release removes the client from the registry and closes it, once its holders are done.
func Release(client *libmangal.Client) error {
	clientsMu.Lock()
	closeNow := false
	for id, e := range clients {
		select {
		case <-e.done:
			if e.client == client {
				closeNow = retire(id, e)
			}
		default:
		}
	}
	clientsMu.Unlock()
	if !closeNow {
		return nil
	}
	if err := client.Close(); err != nil {
		return fmt.Errorf("close %s: %w", client, err)
	}
	return nil
}

// ReleaseIfAuthChanged drops the provider's client when the logins or secrets changed
// since it was made (e.g. the MangaPlus secret set from a TUI while `mangal serve` runs),
// so the next Acquire makes it with the new ones; it's closed once its holders are done.
// It's left alone while anything holds the provider's download lock. True when it was
// dropped.
func ReleaseIfAuthChanged(provider string) (bool, error) {
	current, err := authFingerprint()
	if err != nil {
		return false, err
	}
	clientsMu.Lock()
	e, ok := clients[provider]
	if ok {
		select {
		case <-e.done:
		default:
			ok = false // still being made
		}
	}
	if !ok || e.err != nil || e.auth == current {
		clientsMu.Unlock()
		return false, nil
	}
	if busy, err := providerLocked(provider); err != nil || busy {
		clientsMu.Unlock()
		return false, err
	}
	closeNow := retire(provider, e)
	clientsMu.Unlock()
	if closeNow {
		if err := e.client.Close(); err != nil {
			return true, fmt.Errorf("close %s: %w", e.client, err)
		}
	}
	return true, nil
}

// providerLocked is true when something holds the provider's download lock (a check, a
// download, in any process).
var providerLocked = func(provider string) (bool, error) {
	st, err := cache.Storage()
	if err != nil {
		return false, err
	}
	_, held, err := st.Watches().Holder(ProviderLockName(provider), ProviderLockStale)
	return held, err
}

// CloseAll empties the registry, so the next Acquire makes new clients (e.g. after a
// config change): the idle clients are closed now, the held ones (Hold) when their last
// holder is done. Clients still being created are left to their creators.
func CloseAll() error {
	return closeAll(false)
}

// Shutdown closes every client, held ones too: for mangal's exit.
func Shutdown() error {
	return closeAll(true)
}

func closeAll(force bool) error {
	clientsMu.Lock()
	var toClose []*libmangal.Client
	for id, e := range clients {
		select {
		case <-e.done:
			if retire(id, e) {
				toClose = append(toClose, e.client)
			}
		default:
		}
	}
	if force {
		for e := range lingering {
			delete(lingering, e)
			if !e.closed {
				e.closed = true
				toClose = append(toClose, e.client)
			}
		}
	}
	clientsMu.Unlock()

	var errs []error
	for _, client := range toClose {
		if err := client.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close %s: %w", client, err))
		}
	}
	return errors.Join(errs...)
}

func newClient(ctx context.Context, loader libmangal.ProviderLoader) (*libmangal.Client, error) {
	options := libmangal.DefaultClientOptions()
	options.FS = afs.Afero
	// cover and banner urls come from the sites
	options.HTTPClient = providerloader.ProviderHTTPClient()
	options.UserAgent = config.Download.UserAgent.Get()
	options.ModeDir = config.Download.ModeDir.Get()
	options.ModeFile = config.Download.ModeFile.Get()
	options.ProviderName = template.Provider
	options.MangaName = template.Manga
	options.VolumeName = template.Volume
	options.ChapterName = template.Chapter
	library, err := cache.Library()
	if err != nil {
		return nil, fmt.Errorf("open data db: %w", err)
	}
	index := libraryIndex{library: library, temp: path.Temp()}
	options.MangaDirIndex = index
	options.ChapterIndex = index

	metas, err := metadataProviders()
	if err != nil {
		return nil, err
	}

	client, err := libmangal.NewClientContext(ctx, loader, options)
	if err != nil {
		return nil, fmt.Errorf("load provider %q: %w", loader.Info().ID, err)
	}
	// every mode gets the provider's logs (page lines at debug)
	client.Logger().SetOnLog(log.ProviderHook(""))
	// no login here, see the EnsureLogin of anilist, kitsu and myanimelist
	for _, meta := range metas {
		if err := client.SetMetadataProvider(meta); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("set %s: %w", meta, err)
		}
	}
	return client, nil
}

// metadataProviders are the metadata providers of the clients, by priority: libmangal
// searches the next one only when the previous finds nothing (or fails).
func metadataProviders() ([]*metadata.ProviderWithCache, error) {
	ani, err := anilist.Anilist()
	if err != nil {
		return nil, err
	}
	metas := []*metadata.ProviderWithCache{ani}
	if config.Metadata.Kitsu.Enabled.Get() {
		k, err := kitsu.Kitsu()
		if err != nil {
			return nil, err
		}
		metas = append(metas, k)
	}
	if myanimelist.Configured() {
		m, err := myanimelist.MyAnimeList()
		if err != nil {
			return nil, err
		}
		metas = append(metas, m)
	}
	return metas, nil
}

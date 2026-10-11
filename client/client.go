package client

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	providerloader "github.com/luevano/mangal/v5/provider/loader"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/template"
	"github.com/luevano/mangal/v5/util/afs"
	"github.com/luevano/mangal/v5/util/cache"
)

// entry is a client of the registry, done is closed once it's created (or failed).
type entry struct {
	done   chan struct{}
	client *libmangal.Client
	err    error
}

var (
	clients   = map[string]*entry{}
	clientsMu sync.Mutex

	// create is swapped in tests
	create = newClient
)

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
// client per provider: callers share it and don't close it (see Release and CloseAll).
func Acquire(ctx context.Context, loader libmangal.ProviderLoader) (*libmangal.Client, error) {
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
			e.client, e.err = create(ctx, loader)
			if e.err != nil {
				clientsMu.Lock()
				if clients[id] == e {
					delete(clients, id)
				}
				clientsMu.Unlock()
			}
			close(e.done)
			return e.client, e.err
		}

		select {
		case <-e.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if e.err == nil {
			return e.client, nil
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
		return nil, warnings, fmt.Errorf("provider with id %q not found", id)
	}
	if alias != nil {
		info := loader.Info()
		warnings = append(warnings, fmt.Sprintf("provider %q is now %q (%s), use that id instead", id, info.ID, info.Name))
	}
	if notice, _, err := cache.OldIDsNotice(loaders); err == nil && notice != "" {
		warnings = append(warnings, notice)
	}
	for _, w := range warnings {
		log.Log("%s", w)
	}

	client, err := Acquire(ctx, loader)
	if err != nil {
		return nil, warnings, err
	}
	return client, warnings, nil
}

// Release closes the client and removes it from the registry.
func Release(client *libmangal.Client) error {
	clientsMu.Lock()
	for id, e := range clients {
		select {
		case <-e.done:
			if e.client == client {
				delete(clients, id)
			}
		default:
		}
	}
	clientsMu.Unlock()
	if err := client.Close(); err != nil {
		return fmt.Errorf("close %s: %w", client, err)
	}
	return nil
}

// CloseAll closes every client, even if some fail. Clients still being created are left
// to their creators.
func CloseAll() error {
	clientsMu.Lock()
	var ready []*libmangal.Client
	for id, e := range clients {
		select {
		case <-e.done:
			ready = append(ready, e.client)
			delete(clients, id)
		default:
		}
	}
	clientsMu.Unlock()

	var errs []error
	for _, client := range ready {
		if err := client.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close %s: %w", client, err))
		}
	}
	return errors.Join(errs...)
}

func newClient(ctx context.Context, loader libmangal.ProviderLoader) (*libmangal.Client, error) {
	options := libmangal.DefaultClientOptions()
	options.FS = afs.Afero
	options.HTTPClient = providerloader.HTTPClient()
	options.UserAgent = config.Download.UserAgent.Get()
	options.ModeDir = config.Download.ModeDir.Get()
	options.ModeFile = config.Download.ModeFile.Get()
	options.ProviderName = template.Provider
	options.MangaName = template.Manga
	options.VolumeName = template.Volume
	options.ChapterName = template.Chapter
	_, dataDB, err := cache.DBs()
	if err != nil {
		return nil, fmt.Errorf("open data db: %w", err)
	}
	library := storage.Library{DB: dataDB}
	options.MangaDirIndex = library
	options.ChapterIndex = library

	ani, err := anilist.Anilist()
	if err != nil {
		return nil, err
	}

	client, err := libmangal.NewClientContext(ctx, loader, options)
	if err != nil {
		return nil, fmt.Errorf("load provider %q: %w", loader.Info().ID, err)
	}
	// no login here, see anilist.EnsureLogin
	if err := client.SetMetadataProvider(ani); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("set anilist: %w", err)
	}
	return client, nil
}

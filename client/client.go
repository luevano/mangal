package client

import (
	"context"
	"fmt"
	"github.com/luevano/mangal/storage"
	"github.com/luevano/mangal/util/cache"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/client/anilist"
	"github.com/luevano/mangal/config"
	"github.com/luevano/mangal/log"
	"github.com/luevano/mangal/provider/manager"
	"github.com/luevano/mangal/template"
	"github.com/luevano/mangal/util/afs"
	"github.com/zyedidia/generic/queue"
)

var (
	// TODO: change data structure to be able to close
	// individual clients and remove them from the structure,
	// why was this a queue??
	clients = queue.New[*libmangal.Client]()
	m       sync.Mutex
)

func Get(loader libmangal.ProviderLoader) *libmangal.Client {
	m.Lock()
	defer m.Unlock()

	var client *libmangal.Client
	clients.Each(func(t *libmangal.Client) {
		if t.Info().ID == loader.Info().ID {
			client = t
		}
	})
	return client
}

func Exists(loader libmangal.ProviderLoader) bool {
	m.Lock()
	defer m.Unlock()

	exists := false
	clients.Each(func(client *libmangal.Client) {
		if client.Info().ID == loader.Info().ID {
			exists = true
		}
	})
	return exists
}

func CloseAll() error {
	m.Lock()
	defer m.Unlock()

	for !clients.Empty() {
		client := clients.Peek()
		if err := client.Close(); err != nil {
			return err
		}

		clients.Dequeue()
	}

	return nil
}

func NewClient(ctx context.Context, loader libmangal.ProviderLoader) (*libmangal.Client, error) {
	if Exists(loader) {
		return nil, fmt.Errorf("client for loader %q already exists", loader)
	}
	m.Lock()
	defer m.Unlock()

	HTTPClient := &http.Client{
		Timeout: time.Minute,
	}

	options := libmangal.DefaultClientOptions()
	options.FS = afs.Afero
	options.HTTPClient = HTTPClient
	options.UserAgent = config.Download.UserAgent.Get()
	options.ModeDir = config.Download.ModeDir.Get()
	options.ModeFile = config.Download.ModeFile.Get()
	options.ProviderName = template.Provider
	options.MangaName = template.Manga
	options.VolumeName = template.Volume
	options.ChapterName = template.Chapter
	_, dataDB, err := cache.DBs()
	if err != nil {
		return nil, err
	}
	options.MangaDirIndex = storage.MangaDirs{DB: dataDB}

	client, err := libmangal.NewClientContext(ctx, loader, options)
	if err != nil {
		return nil, err
	}
	// guaranteed to exist
	// anilist.Anilist().SetLogger(client.Logger())
	_ = client.SetMetadataProvider(anilist.Anilist())

	clients.Enqueue(client)
	return client, nil
}

func NewClientByID(ctx context.Context, provider string) (*libmangal.Client, error) {
	loaders, err := manager.Loaders()
	if err != nil {
		return nil, err
	}

	loader, alias, ok := libmangal.FindLoader(loaders, provider)
	if !ok {
		return nil, fmt.Errorf("provider with ID %q not found", provider)
	}
	if alias != nil {
		info := loader.Info()
		msg := fmt.Sprintf("provider %q is now %q (%s), use that id instead", provider, info.ID, info.Name)
		log.Log(msg)
		fmt.Fprintln(os.Stderr, "warning: "+msg)
	}
	if notice, _, err := cache.OldIDsNotice(loaders); err == nil && notice != "" {
		log.Log(notice)
		fmt.Fprintln(os.Stderr, "warning: "+notice)
	}

	client, err := NewClient(ctx, loader)
	if err != nil {
		return nil, err
	}

	return client, nil
}

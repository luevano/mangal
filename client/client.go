package client

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/afs"
	"github.com/luevano/mangal/anilist"
	"github.com/luevano/mangal/provider/manager"
	"github.com/luevano/mangal/template"
	"github.com/samber/lo"
	"github.com/zyedidia/generic/queue"
)

var (
	clients = queue.New[*libmangal.Client]()
	m       sync.Mutex
)

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
	HTTPClient := &http.Client{
		Timeout: time.Minute,
	}

	options := libmangal.DefaultClientOptions()
	options.FS = afs.Afero
	options.Anilist = anilist.Client
	options.HTTPClient = HTTPClient
	options.MangaNameTemplate = template.Manga
	options.VolumeNameTemplate = template.Volume
	options.ChapterNameTemplate = template.Chapter

	client, err := libmangal.NewClient(ctx, loader, options)
	if err != nil {
		return nil, err
	}

	clients.Enqueue(client)
	return client, nil
}

func NewClientByID(ctx context.Context, provider string) (*libmangal.Client, error) {
	loaders, err := manager.Loaders()
	if err != nil {
		return nil, err
	}

	loader, ok := lo.Find(loaders, func(loader libmangal.ProviderLoader) bool {
		return loader.Info().ID == provider
	})

	if !ok {
		return nil, fmt.Errorf("provider with ID %q not found", provider)
	}

	client, err := NewClient(ctx, loader)
	if err != nil {
		return nil, err
	}

	return client, nil
}

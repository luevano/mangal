package watch

import (
	"context"
	"sync"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/util/cache"
)

// ClientSource is the provider's shared client, held until the Source is closed
// (CloseSource; client.HoldByID: closing every client elsewhere doesn't close it under a
// running check). Its cached chapter lists are dropped: a check needs the current ones.
func ClientSource(ctx context.Context, provider string) (Source, error) {
	c, _, release, err := client.HoldByID(ctx, provider)
	if err != nil {
		return nil, err
	}
	if err := cache.ClearChapterCache(c.Info().ID); err != nil {
		release()
		return nil, err
	}
	return clientSource{c: c, release: sync.OnceFunc(release)}, nil
}

// CloseSource is done with the source: a held client is released.
func CloseSource(src Source) {
	if c, ok := src.(interface{ Close() }); ok {
		c.Close()
	}
}

type clientSource struct {
	c       *libmangal.Client
	release func()
}

func (s clientSource) Close() { s.release() }

func (s clientSource) Info() libmangal.ProviderInfo { return s.c.Info() }

func (s clientSource) MangaByID(ctx context.Context, id string) (mangadata.Manga, bool, error) {
	return s.c.MangaByIDContext(ctx, id)
}

func (s clientSource) SearchMangas(ctx context.Context, query string) ([]mangadata.Manga, error) {
	return s.c.SearchMangasContext(ctx, query)
}

func (s clientSource) Chapters(ctx context.Context, manga mangadata.Manga) ([]mangadata.Chapter, error) {
	return client.MangaChapters(ctx, s.c, manga)
}

func (s clientSource) BoundMetadata(ctx context.Context, code metadata.IDCode, id string) (metadata.Metadata, error) {
	return client.BoundMetadata(ctx, s.c, code, id)
}

func (s clientSource) Download(ctx context.Context, chapters []mangadata.Chapter, onEvent func(client.Event)) client.Results {
	options := config.DownloadOptions()
	if options.SearchMetadata {
		_ = anilist.EnsureLogin(ctx)
	}
	return client.Download(ctx, s.c, chapters, options, onEvent)
}

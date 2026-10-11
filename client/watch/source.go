package watch

import (
	"context"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/util/cache"
)

// ClientSource is the provider's shared client (client.AcquireByID). Its cached chapter
// lists are dropped: a check needs the current ones.
func ClientSource(ctx context.Context, provider string) (Source, error) {
	c, _, err := client.AcquireByID(ctx, provider)
	if err != nil {
		return nil, err
	}
	if err := cache.ClearChapterCache(c.Info().ID); err != nil {
		return nil, err
	}
	return clientSource{c}, nil
}

type clientSource struct {
	c *libmangal.Client
}

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

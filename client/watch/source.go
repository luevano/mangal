package watch

import (
	"context"
	"fmt"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/config"
)

// ClientSource is the provider's shared client (client.AcquireByID).
func ClientSource(ctx context.Context, provider string) (Source, error) {
	c, _, err := client.AcquireByID(ctx, provider)
	if err != nil {
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

func (s clientSource) BindAnilist(ctx context.Context, manga mangadata.Manga, anilistID int) error {
	ani, err := s.c.GetMetadataProvider(metadata.IDSourceAnilist)
	if err != nil {
		return err
	}
	_ = anilist.EnsureLogin(ctx)
	meta, found, err := ani.SearchByIDContext(ctx, anilistID)
	if err != nil {
		return fmt.Errorf("get anilist manga %d: %w", anilistID, err)
	}
	if !found {
		return fmt.Errorf("anilist manga %d not found", anilistID)
	}
	manga.SetMetadata(meta)
	return nil
}

func (s clientSource) Download(ctx context.Context, chapters []mangadata.Chapter, bound bool, onEvent func(client.Event)) client.Results {
	options := config.DownloadOptions()
	if bound {
		// searching again would replace the bound metadata
		options.SearchMetadata = false
	}
	if options.SearchMetadata {
		_ = anilist.EnsureLogin(ctx)
	}
	return client.Download(ctx, s.c, chapters, options, onEvent)
}

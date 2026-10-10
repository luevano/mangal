package web

import (
	"context"
	"fmt"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
)

func searchMangas(ctx context.Context, client *libmangal.Client, query string) ([]mangadata.Manga, error) {
	return client.SearchMangasContext(ctx, query)
}

func mangaVolumes(ctx context.Context, client *libmangal.Client, query, mangaID string) ([]mangadata.Volume, error) {
	mangas, err := searchMangas(ctx, client, query)
	if err != nil {
		return nil, err
	}

	for _, manga := range mangas {
		if manga.Info().ID == mangaID {
			return client.MangaVolumesContext(ctx, manga)
		}
	}

	return nil, fmt.Errorf("manga %q not found", mangaID)
}

func volumeChapters(ctx context.Context, client *libmangal.Client, query, mangaID string, volumeNumber float32) ([]mangadata.Chapter, error) {
	volumes, err := mangaVolumes(ctx, client, query, mangaID)
	if err != nil {
		return nil, err
	}

	for _, volume := range volumes {
		if volume.Info().Number == volumeNumber {
			return client.VolumeChaptersContext(ctx, volume)
		}
	}

	return nil, fmt.Errorf("volume %.1f not found", volumeNumber)
}

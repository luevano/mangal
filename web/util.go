package web

import (
	"context"
	"fmt"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/web/api"
)

// findLoader is the provider with the id, which may be an old id of it.
func (s *Server) findLoader(id string) (libmangal.ProviderLoader, error) {
	loader, alias, ok := libmangal.FindLoader(s.loaders, id)
	if !ok {
		return nil, fmt.Errorf("provider %q: %w", id, errNotFound)
	}
	if alias != nil {
		info := loader.Info()
		log.Warn("web: provider %q is now %q (%s), use that id instead", id, info.ID, info.Name)
	}
	return loader, nil
}

// withClient runs fn with the client of the provider with the id.
func withClient[T any](ctx context.Context, s *Server, id string, fn func(*libmangal.Client) (T, error)) (T, error) {
	var zero T
	loader, err := s.findLoader(id)
	if err != nil {
		return zero, err
	}
	c, err := client.Acquire(ctx, loader)
	if err != nil {
		return zero, err
	}
	return fn(c)
}

// findManga searches the query and returns the result with the manga id.
func findManga(ctx context.Context, c *libmangal.Client, query, mangaID string) (mangadata.Manga, error) {
	mangas, err := c.SearchMangasContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("search %q: %w", query, err)
	}
	for _, manga := range mangas {
		if manga.Info().ID == mangaID {
			return manga, nil
		}
	}
	return nil, fmt.Errorf("manga %q in the results of %q: %w", mangaID, query, errNotFound)
}

func findVolume(ctx context.Context, c *libmangal.Client, manga mangadata.Manga, number float32) (mangadata.Volume, error) {
	volumes, err := c.MangaVolumesContext(ctx, manga)
	if err != nil {
		return nil, fmt.Errorf("get volumes of %q: %w", manga, err)
	}
	for _, volume := range volumes {
		if volume.Info().Number == number {
			return volume, nil
		}
	}
	return nil, fmt.Errorf("volume %v of %q: %w", number, manga, errNotFound)
}

func toAPIManga(manga mangadata.Manga) api.Manga {
	info := manga.Info()
	return api.Manga{
		Banner: &info.Banner,
		Cover:  &info.Cover,
		Id:     info.ID,
		Title:  info.Title,
		Url:    &info.URL,
	}
}

func toAPIVolume(volume mangadata.Volume) api.Volume {
	return api.Volume{Number: volume.Info().Number}
}

func toAPIChapter(chapter mangadata.Chapter) api.Chapter {
	info := chapter.Info()
	return api.Chapter{
		Number: info.Number,
		Title:  info.Title,
		Url:    &info.URL,
	}
}

func toAPIProvider(loader libmangal.ProviderLoader) api.Provider {
	info := loader.Info()
	return api.Provider{
		Description: &info.Description,
		Id:          info.ID,
		Name:        info.Name,
		Version:     info.Version,
	}
}

// mapSlice is lo.Map without the index.
func mapSlice[T, R any](items []T, f func(T) R) []R {
	out := make([]R, len(items))
	for i, item := range items {
		out[i] = f(item)
	}
	return out
}

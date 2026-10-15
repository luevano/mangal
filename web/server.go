package web

import (
	"context"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/meta"
	"github.com/luevano/mangal/v5/web/api"
)

var _ api.StrictServerInterface = (*Server)(nil)

type Server struct {
	loaders    []libmangal.ProviderLoader
	images     *imageCache
	httpClient *http.Client
}

// findClosest is swapped in tests.
var findClosest = anilist.FindClosest

// GetMangaPage implements api.StrictServerInterface.
func (s *Server) GetMangaPage(ctx context.Context, request api.GetMangaPageRequestObject) (api.GetMangaPageResponseObject, error) {
	return withClient(ctx, s, request.Params.Provider, func(c *libmangal.Client) (api.GetMangaPageResponseObject, error) {
		manga, err := findManga(ctx, c, request.Params.Query, request.Params.Manga)
		if err != nil {
			return nil, err
		}
		chapters, err := client.MangaChapters(ctx, c, manga)
		if err != nil {
			return nil, err
		}

		response := api.GetMangaPage200JSONResponse{
			Manga:   toAPIManga(manga),
			Volumes: []api.VolumeWithChapters{},
		}
		for _, chapter := range chapters {
			number := chapter.Volume().Info().Number
			if n := len(response.Volumes); n == 0 || response.Volumes[n-1].Volume.Number != number {
				response.Volumes = append(response.Volumes, api.VolumeWithChapters{
					Volume:   api.Volume{Number: number},
					Chapters: []api.Chapter{},
				})
			}
			last := &response.Volumes[len(response.Volumes)-1]
			last.Chapters = append(last.Chapters, toAPIChapter(chapter))
		}

		// the page works without it
		if ani, _, found, err := findClosest(ctx, manga.Info().Title); err == nil && found {
			response.AnilistManga = &api.AnilistManga{
				BannerImage: &ani.BannerImage,
				CoverImage: api.CoverImage{
					Color:      ani.CoverImage.Color,
					ExtraLarge: ani.CoverImage.ExtraLarge,
					Large:      ani.CoverImage.Large,
					Medium:     ani.CoverImage.Medium,
				},
				Description: &ani.Summary,
			}
		}

		return response, nil
	})
}

// GetFormats implements api.StrictServerInterface.
func (*Server) GetFormats(ctx context.Context, request api.GetFormatsRequestObject) (api.GetFormatsResponseObject, error) {
	return api.GetFormats200JSONResponse(mapSlice(libmangal.FormatValues(), func(format libmangal.Format) api.Format {
		f := api.Format{Name: format.String()}
		if extension := format.Extension(); extension != "" {
			f.Extension = &extension
		}
		return f
	})), nil
}

// GetProvider implements api.StrictServerInterface.
func (s *Server) GetProvider(ctx context.Context, request api.GetProviderRequestObject) (api.GetProviderResponseObject, error) {
	loader, err := s.findLoader(request.Params.Id)
	if err != nil {
		return nil, err
	}
	return api.GetProvider200JSONResponse(toAPIProvider(loader)), nil
}

// GetProviders implements api.StrictServerInterface.
func (s *Server) GetProviders(ctx context.Context, _ api.GetProvidersRequestObject) (api.GetProvidersResponseObject, error) {
	return api.GetProviders200JSONResponse(mapSlice(s.loaders, toAPIProvider)), nil
}

// GetMangaVolumes implements api.StrictServerInterface.
func (s *Server) GetMangaVolumes(ctx context.Context, request api.GetMangaVolumesRequestObject) (api.GetMangaVolumesResponseObject, error) {
	return withClient(ctx, s, request.Params.Provider, func(c *libmangal.Client) (api.GetMangaVolumesResponseObject, error) {
		manga, err := findManga(ctx, c, request.Params.Query, request.Params.Manga)
		if err != nil {
			return nil, err
		}
		volumes, err := c.MangaVolumesContext(ctx, manga)
		if err != nil {
			return nil, fmt.Errorf("get volumes of %q: %w", manga, err)
		}
		return api.GetMangaVolumes200JSONResponse(mapSlice(volumes, toAPIVolume)), nil
	})
}

// GetVolumeChapters implements api.StrictServerInterface.
func (s *Server) GetVolumeChapters(ctx context.Context, request api.GetVolumeChaptersRequestObject) (api.GetVolumeChaptersResponseObject, error) {
	return withClient(ctx, s, request.Params.Provider, func(c *libmangal.Client) (api.GetVolumeChaptersResponseObject, error) {
		manga, err := findManga(ctx, c, request.Params.Query, request.Params.Manga)
		if err != nil {
			return nil, err
		}
		volume, err := findVolume(ctx, c, manga, request.Params.Volume)
		if err != nil {
			return nil, err
		}
		chapters, err := c.VolumeChaptersContext(ctx, volume)
		if err != nil {
			return nil, fmt.Errorf("get chapters of %q: %w", volume, err)
		}
		return api.GetVolumeChapters200JSONResponse(mapSlice(chapters, toAPIChapter)), nil
	})
}

// GetMangalInfo implements api.StrictServerInterface.
func (*Server) GetMangalInfo(ctx context.Context, request api.GetMangalInfoRequestObject) (api.GetMangalInfoResponseObject, error) {
	return api.GetMangalInfo200JSONResponse{
		Version: meta.Version,
	}, nil
}

// SearchMangas implements api.StrictServerInterface.
func (s *Server) SearchMangas(ctx context.Context, request api.SearchMangasRequestObject) (api.SearchMangasResponseObject, error) {
	return withClient(ctx, s, request.Params.Provider, func(c *libmangal.Client) (api.SearchMangasResponseObject, error) {
		mangas, err := c.SearchMangasContext(ctx, request.Params.Query)
		if err != nil {
			return nil, fmt.Errorf("search %q: %w", request.Params.Query, err)
		}
		return api.SearchMangas200JSONResponse(mapSlice(mangas, toAPIManga)), nil
	})
}

// newHandler is the API under /api and the UI.
func newHandler(server *Server) (*echo.Echo, error) {
	ui, err := frontend()
	if err != nil {
		return nil, err
	}

	e := echo.New()
	e.HTTPErrorHandler = apiError
	e.Use(imageHeaders)
	api.RegisterHandlersWithBaseURL(e, api.NewStrictHandler(server, nil), "api")
	e.StaticFS("/", ui)
	return e, nil
}

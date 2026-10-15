package web

import (
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/luevano/libmangal"
)

// Server has the providers, the image proxy and the web UI (the API v1 is serve's).
type Server struct {
	loaders    []libmangal.ProviderLoader
	images     *imageCache
	httpClient *http.Client
}

// RegisterUI serves the web UI on /.
func (s *Server) RegisterUI(e *echo.Echo) error {
	ui, err := frontend()
	if err != nil {
		return err
	}
	e.StaticFS("/", ui)
	return nil
}

// Loaders are the providers.
func (s *Server) Loaders() []libmangal.ProviderLoader { return s.loaders }

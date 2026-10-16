package web

import (
	"net/http"
	"sync"

	"github.com/labstack/echo/v5"
	"github.com/luevano/libmangal"
)

// Server has the providers, the image proxy and the web UI (the API v1 is serve's).
type Server struct {
	mu         sync.RWMutex
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
func (s *Server) Loaders() []libmangal.ProviderLoader {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loaders
}

// Reload loads the providers again (updated, or their options changed); the problems are
// the providers that couldn't be loaded. On an error the loaded ones stay.
func (s *Server) Reload() (problems []error, err error) {
	loaders, problems, err := loadProviders()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.loaders = loaders
	s.mu.Unlock()
	return problems, nil
}

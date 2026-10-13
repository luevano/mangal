package web

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/web/api"
)

var (
	// errNotFound is matched by the errors about a provider, manga or volume that isn't there.
	errNotFound = errors.New("not found")
	// errBadRequest is matched by the errors about an invalid parameter.
	errBadRequest = errors.New("bad request")
	// errUpstream is matched by the errors about what a site answered (e.g. not an image).
	errUpstream = errors.New("bad upstream response")
)

// apiError writes the handler's error as an api.Error, with the status it matches.
func apiError(err error, c echo.Context) {
	if c.Response().Committed {
		return
	}

	status := http.StatusInternalServerError
	wait, limited := client.RetryAfter(err)
	var httpErr *echo.HTTPError
	switch {
	case errors.As(err, &httpErr):
		status = httpErr.Code
	case errors.Is(err, errNotFound), errors.Is(err, libmangal.ErrNotFound):
		status = http.StatusNotFound
	case limited:
		status = http.StatusTooManyRequests
		if wait > 0 {
			c.Response().Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
		}
	case errors.Is(err, errBadRequest):
		status = http.StatusBadRequest
	case errors.Is(err, errUpstream):
		status = http.StatusBadGateway
	case errors.Is(err, context.Canceled):
		// the client left or the server is stopping
		log.Debug("web: %s %s: %s", c.Request().Method, c.Request().URL, err)
		status = http.StatusServiceUnavailable
	}

	message := err.Error()
	if httpErr != nil {
		if m, ok := httpErr.Message.(string); ok {
			message = m
		}
	}
	switch {
	case status == http.StatusBadGateway:
		log.Warn("web: %s %s: %s", c.Request().Method, c.Request().URL, err)
	case status >= http.StatusInternalServerError && status != http.StatusServiceUnavailable:
		log.Error("web: %s %s: %s", c.Request().Method, c.Request().URL, err)
	}

	if err := c.JSON(status, api.Error{Code: int32(status), Message: message}); err != nil {
		log.Debug("web: write error response: %s", err)
	}
}

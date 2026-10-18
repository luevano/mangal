package web

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/netutil"
	"github.com/luevano/mangal/v5/log"
)

var (
	// ErrNotFound is matched by the errors about a provider, manga or volume that isn't there.
	ErrNotFound = errors.New("not found")
	// ErrBadRequest is matched by the errors about an invalid parameter.
	ErrBadRequest = errors.New("bad request")
	// ErrUpstream is matched by the errors about what a site answered (e.g. not an image).
	ErrUpstream = errors.New("bad upstream response")
)

// APIError writes the handler's error as {code, message}, with the status it matches.
func APIError(c *echo.Context, err error) {
	if resp, _ := echo.UnwrapResponse(c.Response()); resp != nil && resp.Committed {
		return
	}

	status := http.StatusInternalServerError
	wait, limited := netutil.RetryAfterOf(err)
	var httpErr *echo.HTTPError
	switch {
	case errors.As(err, &httpErr):
		status = httpErr.Code
	case echo.StatusCode(err) != 0:
		// echo's own: no route, method not allowed
		status = echo.StatusCode(err)
	case errors.Is(err, ErrNotFound), errors.Is(err, libmangal.ErrNotFound):
		status = http.StatusNotFound
	case limited:
		status = http.StatusTooManyRequests
		SetRetryAfter(c, wait)
	case errors.Is(err, ErrBadRequest):
		status = http.StatusBadRequest
	case errors.Is(err, ErrUpstream):
		status = http.StatusBadGateway
	case errors.Is(err, context.Canceled):
		// the client left or the server is stopping
		log.Debug("web: %s %s: %s", c.Request().Method, c.Request().URL, err)
		status = http.StatusServiceUnavailable
	}

	message := err.Error()
	if httpErr != nil && httpErr.Message != "" {
		message = httpErr.Message
	}
	switch {
	case status == http.StatusBadGateway:
		log.Warn("web: %s %s: %s", c.Request().Method, c.Request().URL, err)
	case status >= http.StatusInternalServerError && status != http.StatusServiceUnavailable:
		log.Error("web: %s %s: %s", c.Request().Method, c.Request().URL, err)
	}

	if err := c.JSON(status, errorBody{Code: int32(status), Message: message}); err != nil {
		log.Debug("web: write error response: %s", err)
	}
}

// errorBody is the body of an error of a route outside the API.
type errorBody struct {
	Code    int32  `json:"code"`
	Message string `json:"message"`
}

// SetRetryAfter sets the Retry-After header to wait, rounded up to seconds; nothing for no wait.
func SetRetryAfter(c *echo.Context, wait time.Duration) {
	if wait > 0 {
		c.Response().Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
	}
}

package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/luevano/libmangal"
)

type waitError struct{ wait time.Duration }

func (e waitError) Error() string                     { return "429" }
func (e waitError) RetryAfterDuration() time.Duration { return e.wait }

// the providers' errors match libmangal's sentinels: not found is a 404, rate limited a
// 429 with Retry-After when the site said how long
func TestAPIErrorStatus(t *testing.T) {
	for _, c := range []struct {
		err        error
		status     int
		retryAfter string
	}{
		{fmt.Errorf("get manga %q: %w", "x", libmangal.ErrNotFound), http.StatusNotFound, ""},
		{fmt.Errorf("get pages: %w", libmangal.ErrRateLimited), http.StatusTooManyRequests, ""},
		{fmt.Errorf("get pages: %w", waitError{1500 * time.Millisecond}), http.StatusTooManyRequests, "2"},
		{fmt.Errorf("broken"), http.StatusInternalServerError, ""},
	} {
		w := httptest.NewRecorder()
		APIError(echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/x", nil), w), c.err)
		if w.Code != c.status || w.Header().Get("Retry-After") != c.retryAfter {
			t.Errorf("%v: %d, Retry-After %q", c.err, w.Code, w.Header().Get("Retry-After"))
		}
	}
}

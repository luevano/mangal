package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/netutil"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/health"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/web"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// The error codes of the API, besides watch's (not_found, exists, busy, run_not_found,
// run_unfinished).
const (
	CodeInvalid         = "invalid"
	CodeUnauthorized    = "unauthorized"
	CodeForbidden       = "forbidden"
	CodePlanChanged     = "plan_changed"
	CodeRefused         = "refused"
	CodeConfirm         = "confirm_required"
	CodeNothingToDo     = "nothing_to_do"
	CodeUnprocessable   = "unprocessable"
	CodeRateLimited     = "rate_limited"
	CodeUnavailable     = "unavailable"
	CodeUnsupportedType = "unsupported_media_type"
	CodeInternal        = "internal"
)

// apiError is an error with its status and code, and what else the answer has.
type apiError struct {
	status int
	code   string
	err    error
	// reasons, plan and confirm go in the answer
	reasons []string
	plan    *core.Plan
	confirm string
}

func (e *apiError) Error() string { return e.err.Error() }
func (e *apiError) Unwrap() error { return e.err }

func newAPIError(status int, code string, format string, a ...any) *apiError {
	return &apiError{status: status, code: code, err: fmt.Errorf(format, a...)}
}

func badRequest(format string, a ...any) error {
	return newAPIError(http.StatusBadRequest, CodeInvalid, format, a...)
}

// unprocessable is a provider's or a manga's error (not the server's), as a 422.
func unprocessable(err error) error {
	if watch.ErrorCode(err) != "" {
		return err
	}
	return &apiError{status: http.StatusUnprocessableEntity, code: CodeUnprocessable, err: err}
}

// AppOptions are what the server's echo app serves.
type AppOptions struct {
	// Guard is what the requests have to pass first (host, origin).
	Guard web.GuardOptions
	// Web serves the web UI.
	Web bool
}

// marks the routes whose errors are the API's ({error, code}), not the browse routes'
type apiRouteKey struct{}

// NewApp is the server's echo app: the API v1 under /api/v1, the current routes as its
// aliases, the web UI with opts.Web; every request guarded (host, origin), authenticated
// (Auth) and, for the API, checked for its operation's scope.
func (a *API) NewApp(opts AppOptions) (*echo.Echo, error) {
	e := echo.NewWithConfig(echo.Config{JSONSerializer: strictJSON{}})
	e.HTTPErrorHandler = a.errorHandler
	e.Pre(echo.WrapMiddleware(web.Guard(opts.Guard)))
	e.Use(a.authenticate, jsonBodies, web.ImageHeaders)

	strict := apiv1.NewStrictHandler(a, []apiv1.StrictMiddlewareFunc{a.checkScope})
	apiRoute := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			c.Set("apiRoute", true)
			return next(c)
		}
	}
	apiv1.RegisterHandlersWithOptions(apiGroup{e, apiRoute}, strict, apiv1.RegisterHandlersOptions{BaseURL: "/api"})
	e.GET("/api/v1", func(c *echo.Context) error { return c.Redirect(http.StatusMovedPermanently, "/api/v1/") }, apiRoute)

	// the routes before the API v1, for watch.Remote and the TUI
	w := &apiv1.ServerInterfaceWrapper{Handler: strict}
	g := apiGroup{e, apiRoute}
	g.GET("/api/status", w.GetStatus)
	g.GET("/api/watches", w.ListWatches)
	g.POST("/api/watches", w.AddWatch)
	g.GET("/api/watches/stale", w.ListStaleWatches)
	g.GET("/api/watches/:id", w.GetWatch)
	g.PATCH("/api/watches/:id", w.UpdateWatch)
	g.DELETE("/api/watches/:id", w.RemoveWatch)
	g.POST("/api/watches/:id/relink", w.RelinkWatch)
	g.POST("/api/check", w.CheckWatches)
	g.GET("/api/runs", w.ListRuns)
	g.GET("/api/runs/:id", w.GetRun)
	g.POST("/api/runs/:id/cancel", w.CancelRun)
	g.DELETE("/api/watch-runs/:id", w.RemoveWatchRun)
	g.GET("/api/health", w.GetHealth)
	g.GET("/api/health/jobs/:id", w.GetHealthJob)
	g.POST("/api/health/fix", a.legacyHealthFix, a.need(ScopeWrite))
	g.GET("/api/events", a.legacyEvents, a.need(ScopeRead))
	a.registerOPDS(e)

	if opts.Web {
		if a.Browser == nil {
			return nil, errors.New("no browser for the web UI")
		}
		if err := a.Browser.RegisterUI(e); err != nil {
			return nil, err
		}
	} else {
		e.RouteNotFound("/*", func(c *echo.Context) error {
			return newAPIError(http.StatusNotFound, watch.CodeNotFound, "not found (the web UI is off, see serve.web)")
		}, apiRoute)
	}
	return e, nil
}

// apiGroup registers routes on e with a middleware (echo's groups add catch-all routes).
type apiGroup struct {
	e  *echo.Echo
	mw echo.MiddlewareFunc
}

func (g apiGroup) add(method, path string, h echo.HandlerFunc, m ...echo.MiddlewareFunc) echo.RouteInfo {
	return g.e.Add(method, path, h, append([]echo.MiddlewareFunc{g.mw}, m...)...)
}

func (g apiGroup) CONNECT(p string, h echo.HandlerFunc, m ...echo.MiddlewareFunc) echo.RouteInfo {
	return g.add(http.MethodConnect, p, h, m...)
}
func (g apiGroup) DELETE(p string, h echo.HandlerFunc, m ...echo.MiddlewareFunc) echo.RouteInfo {
	return g.add(http.MethodDelete, p, h, m...)
}
func (g apiGroup) GET(p string, h echo.HandlerFunc, m ...echo.MiddlewareFunc) echo.RouteInfo {
	return g.add(http.MethodGet, p, h, m...)
}
func (g apiGroup) HEAD(p string, h echo.HandlerFunc, m ...echo.MiddlewareFunc) echo.RouteInfo {
	return g.add(http.MethodHead, p, h, m...)
}
func (g apiGroup) OPTIONS(p string, h echo.HandlerFunc, m ...echo.MiddlewareFunc) echo.RouteInfo {
	return g.add(http.MethodOptions, p, h, m...)
}
func (g apiGroup) PATCH(p string, h echo.HandlerFunc, m ...echo.MiddlewareFunc) echo.RouteInfo {
	return g.add(http.MethodPatch, p, h, m...)
}
func (g apiGroup) POST(p string, h echo.HandlerFunc, m ...echo.MiddlewareFunc) echo.RouteInfo {
	return g.add(http.MethodPost, p, h, m...)
}
func (g apiGroup) PUT(p string, h echo.HandlerFunc, m ...echo.MiddlewareFunc) echo.RouteInfo {
	return g.add(http.MethodPut, p, h, m...)
}
func (g apiGroup) TRACE(p string, h echo.HandlerFunc, m ...echo.MiddlewareFunc) echo.RouteInfo {
	return g.add(http.MethodTrace, p, h, m...)
}

// authenticate identifies the request (Auth): a user (session or basic auth), an API
// token, the local key. The web UI's files and the login are public, the rest answers 401
// without one (OPDS asks for basic auth, the readers' way).
func (a *API) authenticate(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		r := c.Request()
		id, ctx, err := a.Auth.identify(r)
		public := isPublic(r.URL.Path)
		var limited errLoginLimited
		switch {
		case err == nil:
			ctx = withIdentity(ctx, id)
		case errors.As(err, &limited):
			if !public {
				return &apiError{status: http.StatusTooManyRequests, code: CodeRateLimited, err: err}
			}
		case errors.Is(err, errUnauthorized):
			if !public {
				if isOPDS(r.URL.Path) {
					c.Response().Header().Set("WWW-Authenticate", `Basic realm="mangal", charset="UTF-8"`)
				}
				return &apiError{status: http.StatusUnauthorized, code: CodeUnauthorized, err: err}
			}
		default:
			return err
		}
		ctx = context.WithValue(ctx, requestKey{}, r)
		if s := r.Header.Get("Last-Event-ID"); s != "" {
			if n, err := strconv.ParseUint(s, 10, 64); err == nil {
				ctx = context.WithValue(ctx, lastEventIDKey{}, n)
			}
		}
		c.SetRequest(r.WithContext(ctx))
		return next(c)
	}
}

// publicPaths are the API's routes without a login.
var publicPaths = []string{"/api/v1/auth/login", "/api/v1/auth/logout", "/api/v1/auth/me"}

// isPublic is true for the web UI's files and the login: the rest is the API and OPDS.
func isPublic(path string) bool {
	if strings.HasPrefix(path, "/api/") {
		return slices.Contains(publicPaths, path)
	}
	return !isOPDS(path)
}

func isOPDS(path string) bool {
	return path == "/opds" || strings.HasPrefix(path, "/opds/")
}

// need refuses a request without the scope.
func (a *API) need(scope string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if err := scopeError(c.Request(), scope); err != nil {
				return err
			}
			return next(c)
		}
	}
}

// checkScope refuses an operation the request's scopes don't allow: its x-mangal-scope,
// read for a plan (x-mangal-plan without apply).
func (a *API) checkScope(f apiv1.StrictHandlerFunc, operationID string) apiv1.StrictHandlerFunc {
	op, ok := web.OperationByID(operationID)
	scope := ScopeWrite
	if ok {
		scope = op.Scope
	}
	return func(c *echo.Context, request any) (any, error) {
		if scope == ScopePublic {
			return f(c, request)
		}
		need := scope
		if op.Plan {
			if apply, _ := strconv.ParseBool(c.QueryParam("apply")); !apply {
				need = ScopeRead
			}
		}
		if err := scopeError(c.Request(), need); err != nil {
			return nil, err
		}
		return f(c, request)
	}
}

func scopeError(r *http.Request, scope string) error {
	id, ok := identityOf(r.Context())
	if !ok {
		return newAPIError(http.StatusUnauthorized, CodeUnauthorized, "not logged in")
	}
	if id.has(scope) {
		return nil
	}
	web.LogRefused(r, fmt.Sprintf("needs the %s permission (%s)", scope, id))
	return newAPIError(http.StatusForbidden, CodeForbidden, "this needs the %s permission, %s has %s", scope, id, strings.Join(id.Scopes, ", "))
}

// jsonBodies refuses /api requests with a body that isn't JSON: a page of another site
// can only send simple content types without a CORS preflight.
func jsonBodies(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		r := c.Request()
		if !strings.HasPrefix(r.URL.Path, "/api/") || (r.ContentLength == 0 && len(r.TransferEncoding) == 0) {
			return next(c)
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			return next(c)
		}
		if mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mediaType != "application/json" {
			web.LogRefused(r, fmt.Sprintf("content type %q, not JSON", r.Header.Get("Content-Type")))
			return newAPIError(http.StatusUnsupportedMediaType, CodeUnsupportedType, "the body has to be JSON (Content-Type: application/json)")
		}
		return next(c)
	}
}

// strictJSON decodes the bodies strictly (unknown fields are an error, 1 MiB at most).
type strictJSON struct{ echo.DefaultJSONSerializer }

func (strictJSON) Deserialize(c *echo.Context, target any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, c.Request().Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return badRequest("decode the body: %w", err)
	}
	return nil
}

// errorHandler answers the API's errors as {error, code}, the browse routes' as before.
func (a *API) errorHandler(c *echo.Context, err error) {
	if resp, _ := echo.UnwrapResponse(c.Response()); resp != nil && resp.Committed {
		return
	}
	if isAPI, _ := c.Get("apiRoute").(bool); !isAPI && !strings.HasPrefix(c.Request().URL.Path, "/api/v1") {
		var ae *apiError
		if errors.As(err, &ae) && (ae.status == http.StatusUnauthorized || ae.status == http.StatusUnsupportedMediaType) {
			// the middlewares' errors, the same on every route
			writeAPIError(c, err)
			return
		}
		web.APIError(c, err)
		return
	}
	writeAPIError(c, err)
}

// ErrorResponse is the body of an error of the API.
type ErrorResponse struct {
	Error   string     `json:"error"`
	Code    string     `json:"code,omitempty"`
	Reasons []string   `json:"reasons,omitempty"`
	Plan    *core.Plan `json:"plan,omitempty"`
	Confirm string     `json:"confirm,omitempty"`
}

func writeAPIError(c *echo.Context, err error) {
	status, body := errorBody(err)
	r := c.Request()
	switch {
	case errors.Is(err, context.Canceled):
		log.Debug("serve: %s %s: %s", r.Method, r.URL.Path, err)
	case status >= http.StatusInternalServerError && status != http.StatusServiceUnavailable:
		log.Error("serve: %s %s: %s", r.Method, r.URL.Path, err)
	}
	if wait, limited := netutil.RetryAfterOf(err); limited {
		web.SetRetryAfter(c, wait)
	}
	if err := c.JSON(status, body); err != nil {
		log.Debug("serve: write the error: %s", err)
	}
}

// errorBody is the status and body of an error.
func errorBody(err error) (int, ErrorResponse) {
	body := ErrorResponse{Error: err.Error(), Code: watch.ErrorCode(err)}
	var ae *apiError
	var changed *core.PlanChangedError
	var refused *core.RefusedError
	var notFixable *health.NotFixableError
	var httpErr *echo.HTTPError
	_, limited := netutil.RetryAfterOf(err)
	switch {
	case errors.As(err, &ae):
		body.Code, body.Reasons, body.Plan, body.Confirm = ae.code, ae.reasons, ae.plan, ae.confirm
		return ae.status, body
	case errors.As(err, &changed):
		body.Code, body.Plan = CodePlanChanged, &changed.Plan
		return http.StatusConflict, body
	case errors.As(err, &refused):
		body.Code, body.Reasons = CodeRefused, refused.Reasons
		return http.StatusConflict, body
	case errors.Is(err, core.ErrNothingToDo):
		body.Code = CodeNothingToDo
		return http.StatusConflict, body
	case errors.Is(err, config.ErrNoFile):
		body.Code = CodeRefused
		return http.StatusConflict, body
	case errors.Is(err, core.ErrBusy):
		body.Code = watch.CodeBusy
		return http.StatusConflict, body
	case errors.As(err, &notFixable):
		body.Code = CodeUnprocessable
		return http.StatusUnprocessableEntity, body
	case errors.Is(err, health.ErrChoice), errors.Is(err, core.ErrInvalid), errors.Is(err, web.ErrBadRequest):
		body.Code = CodeInvalid
		return http.StatusBadRequest, body
	case watch.NotFound(body.Code):
		return http.StatusNotFound, body
	case body.Code == watch.CodeExists, body.Code == watch.CodeHeld, watch.Busy(body.Code):
		return http.StatusConflict, body
	case errors.Is(err, core.ErrNotFound), errors.Is(err, libmangal.ErrNotFound), errors.Is(err, web.ErrNotFound), errors.Is(err, storage.ErrRunNotFound):
		body.Code = watch.CodeNotFound
		return http.StatusNotFound, body
	case limited:
		body.Code = CodeRateLimited
		return http.StatusTooManyRequests, body
	case errors.Is(err, web.ErrUpstream):
		body.Code = CodeUnprocessable
		return http.StatusBadGateway, body
	case errors.As(err, &httpErr):
		// the generated parameter binding, echo's routing
		if httpErr.Message != "" {
			body.Error = httpErr.Message
		}
		body.Code = statusCode(httpErr.Code)
		return httpErr.Code, body
	case echo.StatusCode(err) != 0:
		status := echo.StatusCode(err)
		body.Code = statusCode(status)
		return status, body
	case errors.Is(err, context.Canceled):
		body.Code = CodeUnavailable
		return http.StatusServiceUnavailable, body
	}
	body.Code = CodeInternal
	return http.StatusInternalServerError, body
}

func statusCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return CodeInvalid
	case http.StatusNotFound:
		return watch.CodeNotFound
	case http.StatusUnsupportedMediaType:
		return CodeUnsupportedType
	case http.StatusRequestEntityTooLarge:
		return CodeInvalid
	}
	return strings.ToLower(strings.ReplaceAll(http.StatusText(status), " ", "_"))
}

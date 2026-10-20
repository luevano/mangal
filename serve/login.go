package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/alexedwards/scs/v2"
	"github.com/luevano/libmangal/netutil"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/web"
	"github.com/luevano/mangal/v5/web/apiv1"
	"golang.org/x/time/rate"
)

// HashPassword is how a user's password is kept (core.HashPassword).
func HashPassword(password string) (string, error) { return core.HashPassword(password) }

// dummyHash is compared for unknown users, so the time doesn't tell which names exist.
var dummyHash = sync.OnceValue(func() string {
	h, _ := argon2id.CreateHash("not a password", argon2id.DefaultParams)
	return h
})

// The session's keys and cookie.
const (
	sessionUser       = "user"
	sessionGeneration = "generation"
	sessionCookie     = "mangal_session"
)

// DefaultSessionLifetime is how long a remembered login lasts (serve.session_lifetime).
const DefaultSessionLifetime = 30 * 24 * time.Hour

// NewSessions are the sessions kept in store (storage.Sessions), a remembered one lasting
// lifetime.
func NewSessions(store scs.Store, lifetime time.Duration) *scs.SessionManager {
	sm := scs.New()
	sm.Store = store
	if lifetime <= 0 {
		lifetime = DefaultSessionLifetime
	}
	sm.Lifetime = lifetime
	sm.Cookie.Name = sessionCookie
	return sm
}

// errLoginLimited is a login refused for now, too many failed ones.
type errLoginLimited struct{ wait time.Duration }

func (e errLoginLimited) Error() string {
	return fmt.Sprintf("too many failed logins, try again in %s", e.wait.Round(time.Second))
}
func (e errLoginLimited) Is(target error) bool { return target == netutil.ErrRateLimited }

// RetryAfterDuration is how long to wait (netutil.RateLimited).
func (e errLoginLimited) RetryAfterDuration() time.Duration { return e.wait }

// limiter slows the failed logins down: per client address (rate) and per user and address
// (a delay doubling with each failure after a few, not a lockout anyone could cause).
type limiter struct {
	mu    sync.Mutex
	ips   map[string]*rate.Limiter
	fails map[string]failures
}

type failures struct {
	n    int
	last time.Time
}

const (
	// a client gets loginBurst failures, then one every loginEvery
	loginBurst = 10
	loginEvery = 12 * time.Second
	// failures of a user before the delay starts, and its most
	freeFailures = 3
	maxDelay     = 5 * time.Minute
	// limiterMax is when the maps are dropped (memory, a scan of many addresses)
	limiterMax = 10000
)

// wait is how long the client has to wait before trying the user's password, 0 for now.
func (l *limiter) wait(ip, key string, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	var wait time.Duration
	if lim := l.ips[ip]; lim != nil {
		if t := lim.TokensAt(now); t < 1 {
			wait = time.Duration((1 - t) * float64(loginEvery))
		}
	}
	if f, ok := l.fails[key]; ok && f.n >= freeFailures {
		delay := time.Duration(math.Min(float64(time.Second)*math.Pow(2, float64(f.n-freeFailures)), float64(maxDelay)))
		if left := delay - now.Sub(f.last); left > wait {
			wait = left
		}
	}
	return wait
}

func (l *limiter) failed(ip, key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.ips == nil || len(l.ips) > limiterMax {
		l.ips = map[string]*rate.Limiter{}
	}
	if l.fails == nil || len(l.fails) > limiterMax {
		l.fails = map[string]failures{}
	}
	lim := l.ips[ip]
	if lim == nil {
		lim = rate.NewLimiter(rate.Every(loginEvery), loginBurst)
		l.ips[ip] = lim
	}
	lim.AllowN(now, 1)
	f := l.fails[key]
	l.fails[key] = failures{n: f.n + 1, last: now}
}

func (l *limiter) succeeded(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
}

// checkPassword is the user if the password is theirs, rate limited per client.
func (a *Auth) checkPassword(r *http.Request, name, password string) (storage.User, error) {
	a.mu.Lock()
	if a.limits == nil {
		a.limits = &limiter{}
	}
	limits := a.limits
	a.mu.Unlock()
	ip := a.clientIP(r)
	key := strings.ToLower(name) + "\x00" + ip
	now := time.Now()
	if wait := limits.wait(ip, key, now); wait > 0 {
		web.LogRefused(r, fmt.Sprintf("login of %q rate limited", name))
		return storage.User{}, errLoginLimited{wait: wait}
	}
	u, err := a.user(name)
	if err != nil && !errors.Is(err, storage.ErrUserNotFound) {
		return storage.User{}, err
	}
	hash := u.Hash
	if err != nil {
		hash = dummyHash()
	}
	match, cerr := argon2id.ComparePasswordAndHash(password, hash)
	if cerr != nil {
		log.Warn("serve: the password hash of %q: %s", name, cerr)
	}
	if err != nil || !match {
		limits.failed(ip, key, now)
		web.LogRefused(r, fmt.Sprintf("wrong name or password (%q)", name))
		return storage.User{}, fmt.Errorf("%w: wrong name or password", errUnauthorized)
	}
	limits.succeeded(key)
	if a.Users != nil {
		if err := a.Users.LoggedIn(u.Name, now); err != nil {
			log.Warn("serve: %s", err)
		}
	}
	return u, nil
}

// requestKey has the request in the context, for the handlers that set cookies.
type requestKey struct{}

func requestOf(ctx context.Context) (*http.Request, bool) {
	r, ok := ctx.Value(requestKey{}).(*http.Request)
	return r, ok
}

// secure is true when the session cookie gets the Secure flag.
func (a *Auth) secure(r *http.Request) bool {
	return a.SecureCookie || r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// sessionCookieOf is the cookie of the session, persistent until expiry when remembered;
// an empty token deletes it.
func (a *Auth) sessionCookieOf(r *http.Request, token string, expiry time.Time, remember bool) *http.Cookie {
	c := &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: a.secure(r)}
	switch {
	case token == "":
		c.MaxAge, c.Expires = -1, time.Unix(1, 0)
	case remember:
		c.Expires = expiry
		c.MaxAge = int(time.Until(expiry).Seconds())
	}
	return c
}

func meOf(id identity) apiv1.Me {
	scopes := id.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	return apiv1.Me{Name: id.Name, Kind: apiv1.MeKind(id.Kind), Scopes: scopes, Admin: id.admin(), HideUnavailable: config.Serve.HideUnavailable.Get()}
}

// loginResponse answers the login with its cookie.
type loginResponse struct {
	cookie *http.Cookie
	me     apiv1.Me
}

func (l loginResponse) VisitLoginResponse(w http.ResponseWriter) error {
	http.SetCookie(w, l.cookie)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(l.me)
}

// logoutResponse answers the logout, the cookie deleted.
type logoutResponse struct{ cookie *http.Cookie }

func (l logoutResponse) VisitLogoutResponse(w http.ResponseWriter) error {
	http.SetCookie(w, l.cookie)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// Login implements apiv1.StrictServerInterface.
func (a *API) Login(ctx context.Context, request apiv1.LoginRequestObject) (apiv1.LoginResponseObject, error) {
	r, ok := requestOf(ctx)
	if !ok || request.Body == nil {
		return nil, badRequest("no request")
	}
	auth := a.Auth
	if auth.Mode == AuthNone {
		return nil, newAPIError(http.StatusConflict, CodeRefused, "serve.auth is none: there's nothing to log into")
	}
	if auth.Sessions == nil {
		return nil, newAPIError(http.StatusServiceUnavailable, CodeUnavailable, "logins aren't available")
	}
	u, err := auth.checkPassword(r, strings.TrimSpace(request.Body.Name), request.Body.Password)
	if err != nil {
		if _, ok := errors.AsType[errLoginLimited](err); ok {
			return nil, &apiError{status: http.StatusTooManyRequests, code: CodeRateLimited, err: err}
		}
		if errors.Is(err, errUnauthorized) {
			return nil, newAPIError(http.StatusUnauthorized, CodeUnauthorized, "wrong name or password")
		}
		return nil, err
	}
	// a new id for the login (session fixation)
	if err := auth.Sessions.RenewToken(ctx); err != nil {
		return nil, err
	}
	remember := request.Body.Remember != nil && *request.Body.Remember
	auth.Sessions.Put(ctx, sessionUser, u.Name)
	auth.Sessions.Put(ctx, sessionGeneration, u.Generation)
	token, expiry, err := auth.Sessions.Commit(ctx)
	if err != nil {
		return nil, err
	}
	if st, ok := auth.Sessions.Store.(interface{ RemoveExpired() error }); ok {
		if err := st.RemoveExpired(); err != nil {
			log.Warn("serve: %s", err)
		}
	}
	log.Info("serve: %s logged in from %s", u.Name, auth.clientIP(r))
	return loginResponse{cookie: auth.sessionCookieOf(r, token, expiry, remember), me: meOf(userIdentity(u))}, nil
}

// Logout implements apiv1.StrictServerInterface.
func (a *API) Logout(ctx context.Context, _ apiv1.LogoutRequestObject) (apiv1.LogoutResponseObject, error) {
	r, ok := requestOf(ctx)
	if !ok {
		return nil, badRequest("no request")
	}
	if a.Auth.Sessions != nil {
		if err := a.Auth.Sessions.Destroy(ctx); err != nil {
			return nil, err
		}
	}
	return logoutResponse{cookie: a.Auth.sessionCookieOf(r, "", time.Time{}, false)}, nil
}

// GetMe implements apiv1.StrictServerInterface.
func (a *API) GetMe(ctx context.Context, _ apiv1.GetMeRequestObject) (apiv1.GetMeResponseObject, error) {
	id, ok := identityOf(ctx)
	if !ok {
		return nil, newAPIError(http.StatusUnauthorized, CodeUnauthorized, "not logged in")
	}
	return apiv1.GetMe200JSONResponse(meOf(id)), nil
}

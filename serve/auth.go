package serve

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/labstack/echo/v5"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/luevano/mangal/v5/web"
)

// The permissions of the users and the scopes of the API tokens (core's).
const (
	ScopeRead     = core.ScopeRead
	ScopeWatch    = core.ScopeWatch
	ScopeProgress = core.ScopeProgress
	ScopeWrite    = core.ScopeWrite
	// ScopePublic is an operation anyone may call (the login).
	ScopePublic = "public"
)

// Scopes are every scope.
var Scopes = core.Scopes

// The auth modes (serve.auth).
const (
	AuthLogin = "login"
	AuthNone  = "none"
)

// NormalizeScopes are the scopes checked, sorted as Scopes, with read when there's any.
func NormalizeScopes(scopes []string) ([]string, error) { return core.NormalizeScopes(scopes) }

// HashToken is how an API token is kept: the hex sha256.
func HashToken(token string) string { return core.HashToken(token) }

// NewToken is a random token (API tokens, the local key).
func NewToken() (string, error) { return core.NewToken() }

// The kinds of identity.
const (
	KindUser  = "user"
	KindToken = "token"
	// KindLocal is mangal on the server's config (the CLI, the TUI): the local key of the
	// discovery file.
	KindLocal = "local"
	KindNone  = "none"
)

// identity is who a request is: a user, an API token, the server's own CLI, or anyone
// with serve.auth none.
type identity struct {
	Name   string
	Kind   string
	Scopes []string
}

func (id identity) has(scope string) bool { return slices.Contains(id.Scopes, scope) }

// admin is every permission: what the users, tokens and admin-only settings need.
func (id identity) admin() bool {
	for _, s := range Scopes {
		if !id.has(s) {
			return false
		}
	}
	return true
}

// String says how the request is authorized, as the status has it.
func (id identity) String() string {
	switch id.Kind {
	case KindNone:
		return "none (serve.auth)"
	case KindLocal:
		return "local (this config's mangal)"
	}
	return id.Kind + " " + id.Name
}

type identityKey struct{}

func withIdentity(ctx context.Context, id identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

func identityOf(ctx context.Context) (identity, bool) {
	id, ok := ctx.Value(identityKey{}).(identity)
	return id, ok
}

// UserStore is where the users are (storage.Users).
type UserStore interface {
	Get(name string) (storage.User, error)
	LoggedIn(name string, at time.Time) error
}

// Auth is how the server authenticates requests.
type Auth struct {
	// Mode is AuthLogin or AuthNone.
	Mode string
	// Users log in with their password (the login page, basic auth), nil for none.
	Users UserStore
	// Sessions are the logins' sessions (the cookie), nil without Users.
	Sessions *scs.SessionManager
	// Tokens are the API tokens (`mangal token`), re-read every few seconds so a
	// revoked one stops working; nil for none.
	Tokens func() ([]cache.ServeTokenEntry, error)
	// LocalKey is the key of the discovery file: mangal on this config (the CLI, the TUI)
	// sends it, it has every scope.
	LocalKey string
	// TrustedProxies are the reverse proxies whose X-Forwarded-For is the client's address
	// (the logs, the login rate limit).
	TrustedProxies []*net.IPNet
	// SecureCookie sets the session cookie's Secure flag for every request, else only for
	// HTTPS ones (TLS, or X-Forwarded-Proto https).
	SecureCookie bool

	mu       sync.Mutex
	tokens   []cache.ServeTokenEntry
	tokensAt time.Time
	users    map[string]cachedUser
	basic    map[string]cachedBasic
	limits   *limiter

	extractOnce sync.Once
	extract     echo.IPExtractor
}

// cachedUser is a user as read at, re-read after usersEvery (another process changed it).
type cachedUser struct {
	user storage.User
	at   time.Time
}

// cachedBasic is a basic auth password that matched the user's generation, so a reader
// app's every request doesn't hash it again.
type cachedBasic struct {
	generation int64
	until      time.Time
}

const (
	// tokensEvery and usersEvery are how long the tokens and users are kept before they're
	// read again.
	tokensEvery = 5 * time.Second
	usersEvery  = 5 * time.Second
	// basicFor is how long a basic auth password is believed without hashing it again.
	basicFor = 10 * time.Minute
)

func (a *Auth) apiTokens() ([]cache.ServeTokenEntry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.Tokens == nil {
		return nil, nil
	}
	if a.tokensAt.IsZero() || time.Since(a.tokensAt) > tokensEvery {
		tokens, err := a.Tokens()
		if err != nil {
			return nil, err
		}
		a.tokens, a.tokensAt = tokens, time.Now()
	}
	return a.tokens, nil
}

// user is the user, storage.ErrUserNotFound when there's none.
func (a *Auth) user(name string) (storage.User, error) {
	if a.Users == nil {
		return storage.User{}, storage.ErrUserNotFound
	}
	key := strings.ToLower(name)
	a.mu.Lock()
	c, ok := a.users[key]
	a.mu.Unlock()
	if ok && time.Since(c.at) < usersEvery {
		return c.user, nil
	}
	u, err := a.Users.Get(name)
	if err != nil {
		// not cached: any name may be sent, the map would grow without bound
		return storage.User{}, err
	}
	a.mu.Lock()
	if a.users == nil {
		a.users = map[string]cachedUser{}
	}
	a.users[key] = cachedUser{user: u, at: time.Now()}
	a.mu.Unlock()
	return u, nil
}

// forget drops the cached user (changed here: it applies at once).
func (a *Auth) forget(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.users, strings.ToLower(name))
}

// forgetTokens drops the cached API tokens.
func (a *Auth) forgetTokens() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tokensAt = time.Time{}
}

// errUnauthorized is a request that isn't logged in (or a wrong password or token).
var errUnauthorized = errors.New("not logged in")

// identify is who the request is, errUnauthorized when nobody (or a wrong token or
// password); the context has the session.
func (a *Auth) identify(r *http.Request) (identity, context.Context, error) {
	ctx := r.Context()
	if a.Mode == AuthNone {
		return identity{Name: AuthNone, Kind: KindNone, Scopes: Scopes}, ctx, nil
	}
	if h := r.Header.Get("Authorization"); h != "" {
		if token, ok := strings.CutPrefix(h, "Bearer "); ok {
			id, err := a.bearer(r, token)
			return id, ctx, err
		}
		if name, password, ok := r.BasicAuth(); ok {
			id, err := a.password(r, name, password)
			return id, ctx, err
		}
	}
	if a.Sessions != nil {
		if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
			sctx, err := a.Sessions.Load(ctx, c.Value)
			if err != nil {
				return identity{}, ctx, err
			}
			if id, ok := a.session(sctx); ok {
				return id, sctx, nil
			}
		}
		// a new session can be started on it (the login)
		sctx, err := a.Sessions.Load(ctx, "")
		if err != nil {
			return identity{}, ctx, err
		}
		ctx = sctx
	}
	return identity{}, ctx, errUnauthorized
}

// bearer is the local key or an API token.
func (a *Auth) bearer(r *http.Request, token string) (identity, error) {
	if a.LocalKey != "" && subtle.ConstantTimeCompare([]byte(token), []byte(a.LocalKey)) == 1 {
		return identity{Name: KindLocal, Kind: KindLocal, Scopes: Scopes}, nil
	}
	tokens, err := a.apiTokens()
	if err != nil {
		return identity{}, err
	}
	hash := HashToken(token)
	for _, t := range tokens {
		if subtle.ConstantTimeCompare([]byte(hash), []byte(t.Hash)) == 1 {
			scopes, _ := NormalizeScopes(t.Scopes)
			return identity{Name: t.Name, Kind: KindToken, Scopes: scopes}, nil
		}
	}
	web.LogRefused(r, "wrong API token")
	return identity{}, fmt.Errorf("%w: wrong API token", errUnauthorized)
}

// password is the user of a basic auth request (OPDS readers, scripts).
func (a *Auth) password(r *http.Request, name, password string) (identity, error) {
	key := HashToken(name + "\x00" + password)
	u, err := a.user(name)
	if err == nil {
		a.mu.Lock()
		c, ok := a.basic[key]
		a.mu.Unlock()
		if ok && c.generation == u.Generation && time.Now().Before(c.until) {
			return userIdentity(u), nil
		}
	}
	u, err = a.checkPassword(r, name, password)
	if err != nil {
		return identity{}, err
	}
	a.mu.Lock()
	if a.basic == nil {
		a.basic = map[string]cachedBasic{}
	}
	a.basic[key] = cachedBasic{generation: u.Generation, until: time.Now().Add(basicFor)}
	a.mu.Unlock()
	return userIdentity(u), nil
}

func userIdentity(u storage.User) identity {
	scopes, _ := NormalizeScopes(u.Perms)
	return identity{Name: u.Name, Kind: KindUser, Scopes: scopes}
}

// session is the user of the session, false when it has none (or an ended one).
func (a *Auth) session(ctx context.Context) (identity, bool) {
	name := a.Sessions.GetString(ctx, sessionUser)
	if name == "" {
		return identity{}, false
	}
	u, err := a.user(name)
	if err != nil || u.Generation != a.Sessions.GetInt64(ctx, sessionGeneration) {
		return identity{}, false
	}
	return userIdentity(u), true
}

// clientIP is the request's client: the X-Forwarded-For of a trusted proxy, else the peer.
func (a *Auth) clientIP(r *http.Request) string {
	if len(a.TrustedProxies) != 0 && web.FromProxy(r, a.TrustedProxies) {
		a.extractOnce.Do(func() {
			opts := []echo.TrustOption{echo.TrustLoopback(false), echo.TrustLinkLocal(false), echo.TrustPrivateNet(false)}
			for _, p := range a.TrustedProxies {
				opts = append(opts, echo.TrustIPRange(p))
			}
			a.extract = echo.ExtractIPFromXFFHeader(opts...)
		})
		return a.extract(r)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ParseProxies are the addresses and CIDRs of serve.trusted_proxies.
func ParseProxies(list []string) ([]*net.IPNet, error) {
	var nets []*net.IPNet
	for _, s := range list {
		if !strings.Contains(s, "/") {
			ip := net.ParseIP(s)
			if ip == nil {
				return nil, fmt.Errorf("serve.trusted_proxies: %q isn't an address or a CIDR", s)
			}
			bits := 128
			if ip.To4() != nil {
				ip, bits = ip.To4(), 32
			}
			nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("serve.trusted_proxies: %w", err)
		}
		nets = append(nets, n)
	}
	return nets, nil
}

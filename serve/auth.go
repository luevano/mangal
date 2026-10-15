package serve

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/luevano/mangal/v5/web"
)

// The scopes of the API's tokens: read is every GET and the events, watch changes the
// watches, write the files, the library, the config, the downloads.
const (
	ScopeRead  = "read"
	ScopeWatch = "watch"
	ScopeWrite = "write"
)

// Scopes are every scope.
var Scopes = []string{ScopeRead, ScopeWatch, ScopeWrite}

// The auth modes (serve.auth).
const (
	AuthToken = "token"
	AuthNone  = "none"
)

// NormalizeScopes are the scopes checked, sorted as Scopes, with read when there's any.
func NormalizeScopes(scopes []string) ([]string, error) {
	var out []string
	for _, s := range scopes {
		s = strings.TrimSpace(strings.ToLower(s))
		if s == "" {
			continue
		}
		if !slices.Contains(Scopes, s) {
			return nil, fmt.Errorf("%q isn't a scope (%s)", s, strings.Join(Scopes, ", "))
		}
		out = append(out, s)
	}
	if len(out) != 0 {
		out = append(out, ScopeRead)
	}
	var sorted []string
	for _, s := range Scopes {
		if slices.Contains(out, s) {
			sorted = append(sorted, s)
		}
	}
	return sorted, nil
}

// HashToken is how a named token is kept: the hex sha256.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NewToken is a random token.
func NewToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// identity is who a request is: a token's name and scopes, or the loopback's scopes.
type identity struct {
	// Name is the token's ("config", "default" or its name), "loopback" without a token
	// from the same machine, "none" with serve.auth = none.
	Name   string
	Scopes []string
	// Anonymous is a request without a token.
	Anonymous bool
}

func (id identity) has(scope string) bool { return slices.Contains(id.Scopes, scope) }

// String says how the request is authorized, as the status has it.
func (id identity) String() string {
	switch {
	case id.Name == AuthNone:
		return "none (serve.auth)"
	case id.Anonymous:
		return "loopback (serve.loopback_scopes)"
	}
	return "token " + id.Name
}

type identityKey struct{}

func withIdentity(ctx context.Context, id identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

func identityOf(ctx context.Context) (identity, bool) {
	id, ok := ctx.Value(identityKey{}).(identity)
	return id, ok
}

// Auth is how the server authenticates requests.
type Auth struct {
	// Mode is AuthToken or AuthNone.
	Mode string
	// Token has every scope (serve.token), TokenName says where it's from.
	Token, TokenName string
	// Saved is the token saved with `mangal serve token` (every scope), re-read with the
	// named ones so one saved while the server runs works; nil for none.
	Saved func() (string, error)
	// LoopbackScopes are the scopes of a request from the same machine without a token.
	LoopbackScopes []string
	// Named are the named tokens (`mangal serve token --name`), re-read every few
	// seconds so a revoked one stops working; nil for none.
	Named func() ([]cache.ServeTokenEntry, error)
	// TrustedProxies are the reverse proxies whose X-Forwarded-For is believed.
	TrustedProxies []*net.IPNet

	mu       sync.Mutex
	saved    string
	named    []cache.ServeTokenEntry
	namedAt  time.Time
	namedErr error

	extractOnce sync.Once
	extract     echo.IPExtractor
}

// namedEvery is how long the named tokens are kept before they're read again.
const namedEvery = 5 * time.Second

// tokens are the saved token and the named ones, read again every few seconds.
func (a *Auth) tokens() (string, []cache.ServeTokenEntry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.namedAt.IsZero() || time.Since(a.namedAt) > namedEvery {
		a.saved, a.named, a.namedErr = "", nil, nil
		if a.Saved != nil {
			a.saved, a.namedErr = a.Saved()
		}
		if a.Named != nil && a.namedErr == nil {
			a.named, a.namedErr = a.Named()
		}
		a.namedAt = time.Now()
	}
	return a.saved, a.named, a.namedErr
}

// HasTokens is true when some token is accepted.
func (a *Auth) HasTokens() bool {
	saved, named, _ := a.tokens()
	return a.Token != "" || saved != "" || len(named) != 0
}

// errUnauthorized is a missing or wrong token.
var errUnauthorized = errors.New("missing or wrong token (serve.token, mangal serve token)")

// identify is who the request is, errUnauthorized when it has no (right) token and isn't
// let in without one.
func (a *Auth) identify(r *http.Request) (identity, error) {
	if a.Mode == AuthNone {
		return identity{Name: AuthNone, Scopes: Scopes}, nil
	}
	token, given := requestToken(r)
	if given {
		if a.Token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(a.Token)) == 1 {
			return identity{Name: a.TokenName, Scopes: Scopes}, nil
		}
		saved, named, err := a.tokens()
		if err != nil {
			return identity{}, err
		}
		if saved != "" && subtle.ConstantTimeCompare([]byte(token), []byte(saved)) == 1 {
			return identity{Name: "default", Scopes: Scopes}, nil
		}
		hash := HashToken(token)
		for _, t := range named {
			if subtle.ConstantTimeCompare([]byte(hash), []byte(t.Hash)) == 1 {
				scopes, _ := NormalizeScopes(t.Scopes)
				return identity{Name: t.Name, Scopes: scopes}, nil
			}
		}
		web.LogRefused(r, "wrong token")
		return identity{}, errUnauthorized
	}
	if len(a.LoopbackScopes) != 0 && a.local(r) {
		return identity{Name: "loopback", Scopes: a.LoopbackScopes, Anonymous: true}, nil
	}
	web.LogRefused(r, "missing token")
	return identity{}, errUnauthorized
}

// requestToken is the token as a bearer token or as the password of basic auth (the web
// UI), given is false without one.
func requestToken(r *http.Request) (token string, given bool) {
	if h := r.Header.Get("Authorization"); h != "" {
		if t, ok := strings.CutPrefix(h, "Bearer "); ok {
			return t, true
		}
	}
	if _, password, ok := r.BasicAuth(); ok {
		return password, true
	}
	return "", false
}

// forwardHeaders are the headers of a request through a proxy.
var forwardHeaders = []string{"X-Forwarded-For", "Forwarded", "X-Real-Ip", "X-Forwarded-Host"}

// local is true for a request from the same machine: from a loopback address for a
// loopback host (localhost, 127.0.0.1), not through a proxy, or through a trusted proxy
// for a client on a loopback address.
func (a *Auth) local(r *http.Request) bool {
	if !web.IsLoopbackHost(r.Host) {
		// a reverse proxy passing its public name
		return false
	}
	if len(a.TrustedProxies) != 0 && web.FromProxy(r, a.TrustedProxies) {
		a.extractOnce.Do(func() {
			opts := []echo.TrustOption{echo.TrustLoopback(false), echo.TrustLinkLocal(false), echo.TrustPrivateNet(false)}
			for _, p := range a.TrustedProxies {
				opts = append(opts, echo.TrustIPRange(p))
			}
			a.extract = echo.ExtractIPFromXFFHeader(opts...)
		})
		ip := net.ParseIP(a.extract(r))
		return ip != nil && ip.IsLoopback()
	}
	for _, h := range forwardHeaders {
		if r.Header.Get(h) != "" {
			return false
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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

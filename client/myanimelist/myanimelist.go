// Package myanimelist is the MyAnimeList metadata provider shared by every client, and its
// login. It needs the client id of a MyAnimeList API client, saved with the logins
// (cache.SetMyAnimeListClient).
package myanimelist

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/libmangal/metadata/myanimelist"
	"github.com/luevano/mangal/v5/auth/oauth"
	"github.com/luevano/mangal/v5/config"
	mlog "github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/provider/loader"
	"github.com/luevano/mangal/v5/util/cache"
	"golang.org/x/oauth2"
)

const (
	// refreshBefore is how long before the token expires it's refreshed (they last ~31 days).
	refreshBefore = 7 * 24 * time.Hour
)

// ErrNotConfigured is MyAnimeList without a client id.
var ErrNotConfigured = errors.New("not configured: set the client id of a MyAnimeList API client (mangal myanimelist client)")

var (
	mal      *myanimelist.MyAnimeList
	provider *metadata.ProviderWithCache
	// providerID is the client id provider was made with, a new one makes a new provider
	providerID string
	malLock    sync.Mutex

	// loginSem serializes the logins, a channel so waiting can be cancelled
	loginSem   = make(chan struct{}, 1)
	loginTried bool

	loginErr     error
	loginErrLock sync.Mutex

	// httpClient is swapped in tests
	httpClient = loader.HTTPClient
)

// Configured is true when MyAnimeList can be used: enabled (metadata.myanimelist.enabled),
// with a saved client id.
func Configured() bool {
	if !config.Metadata.MyAnimeList.Enabled.Get() {
		return false
	}
	id, _, err := cache.MyAnimeListClient()
	if err != nil {
		mlog.Warn("myanimelist client: %s", err)
	}
	return id != ""
}

// MyAnimeList is the MyAnimeList metadata provider shared by every client, ErrNotConfigured
// without a client id. It makes no request: EnsureLogin logs in with the saved user.
func MyAnimeList() (*metadata.ProviderWithCache, error) {
	malLock.Lock()
	defer malLock.Unlock()
	// the client id can change during the run (the TUI, another process for mangal serve)
	id, _, err := cache.MyAnimeListClient()
	if err != nil {
		return nil, fmt.Errorf("get the myanimelist client: %w", err)
	}
	if provider != nil && id == providerID {
		return provider, nil
	}
	if id == "" {
		return nil, ErrNotConfigured
	}

	opts := myanimelist.DefaultOptions()
	opts.ClientID = id
	opts.HTTPClient = httpClient()
	m, err := myanimelist.NewMAL(opts)
	if err != nil {
		return nil, fmt.Errorf("create myanimelist: %w", err)
	}

	cacheOpts := metadata.DefaultProviderWithCacheOptions()
	cacheOpts.Provider = &guarded{MyAnimeList: m}
	cacheOpts.CacheStore = cache.CacheStore
	p, err := metadata.NewProviderWithCache(cacheOpts)
	if err != nil {
		return nil, fmt.Errorf("create myanimelist: %w", err)
	}
	mal, provider, providerID = m, p, id
	return provider, nil
}

// guarded stops searching once MyAnimeList refused the client id: the first search says so,
// the next ones find nothing instead of failing every metadata search.
type guarded struct {
	*myanimelist.MyAnimeList
	refused atomic.Bool
}

func (g *guarded) Search(ctx context.Context, query string) ([]metadata.Metadata, error) {
	if g.refused.Load() {
		return nil, nil
	}
	mangas, err := g.MyAnimeList.Search(ctx, query)
	if errors.Is(err, myanimelist.ErrInvalidClientID) {
		g.refused.Store(true)
		return nil, fmt.Errorf("myanimelist refused the client id (mangal myanimelist client), not searched again until it changes: %w", err)
	}
	return mangas, err
}

func client() (*myanimelist.MyAnimeList, error) {
	if _, err := MyAnimeList(); err != nil {
		return nil, err
	}
	return mal, nil
}

// oauthContext makes the oauth requests use mangal's http client.
func oauthContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, httpClient())
}

// EnsureLogin logs into MyAnimeList with the saved user once per process (see CheckLogin).
// A cancelled attempt is tried again by the next call.
func EnsureLogin(ctx context.Context) error {
	if _, err := MyAnimeList(); err != nil {
		return err
	}
	select {
	case loginSem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-loginSem }()
	if loginTried {
		return nil
	}

	err := checkLogin(ctx)
	if ctx.Err() == nil {
		loginTried = true
	}
	if err != nil {
		mlog.Warn("myanimelist login: %s", err)
	}
	return err
}

// Login is the saved login of the last MyAnimeList user.
type Login struct {
	User    string
	Expires time.Time
	// Err is why the login couldn't be checked this run (e.g. offline).
	Err   error
	token oauth2.Token
}

// Expired is true once the token expired and can't be refreshed (MyAnimeList rejected it).
func (l Login) Expired() bool {
	return l.token.RefreshToken == "" && !l.Expires.IsZero() && time.Now().After(l.Expires)
}

// SavedLogin is the last user's saved login, read locally (no request). There's none after
// a logout, until the next login.
func SavedLogin() (Login, bool, error) {
	if out, err := cache.MyAnimeListLoggedOut(); err != nil || out {
		return Login{}, false, err
	}
	user, err := lastUser()
	if err != nil || user == "" {
		return Login{}, false, err
	}
	var token oauth2.Token
	found, err := cache.GetMyAnimeListAuthData(user, &token)
	if err != nil || !found {
		return Login{}, false, err
	}
	loginErrLock.Lock()
	defer loginErrLock.Unlock()
	return Login{User: user, Expires: token.Expiry, Err: loginErr, token: token}, true, nil
}

// CheckLogin logs into MyAnimeList with the saved login, unless it already is or the login
// expired. The token is refreshed when it expires soon, one MyAnimeList rejects is marked
// expired.
func CheckLogin(ctx context.Context) error {
	select {
	case loginSem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-loginSem }()
	return checkLogin(ctx)
}

func checkLogin(ctx context.Context) error {
	m, err := client()
	if err != nil {
		return err
	}
	if m.Authenticated() {
		return nil
	}
	login, found, err := SavedLogin()
	if err != nil || !found || login.Expired() {
		return err
	}

	err = logInSaved(ctx, m, login.User, login.token)
	loginErrLock.Lock()
	loginErr = nil
	if err != nil && !rejected(err) && ctx.Err() == nil {
		loginErr = err
	}
	loginErrLock.Unlock()
	if err != nil {
		return fmt.Errorf("log in %q: %w", login.User, err)
	}
	return nil
}

// rejected is a token or refresh token MyAnimeList rejected.
func rejected(err error) bool {
	return errors.Is(err, metadata.ErrUnauthorized) || errors.Is(err, oauth.ErrRefreshRejected)
}

// logInSaved logs in with the saved token of the user, refreshed first when it expires
// soon, or when MyAnimeList rejects it. A rejected refresh marks the login expired.
func logInSaved(ctx context.Context, m *myanimelist.MyAnimeList, user string, token oauth2.Token) error {
	refreshed := false
	if token.RefreshToken != "" && !token.Expiry.IsZero() && time.Until(token.Expiry) < refreshBefore {
		if err := refresh(ctx, user, &token); err != nil {
			// offline: the token may still be good
			if rejected(err) || time.Now().After(token.Expiry) {
				return err
			}
		} else {
			refreshed = true
		}
	}

	err := m.Login(ctx, token.AccessToken)
	if errors.Is(err, metadata.ErrUnauthorized) && !refreshed && token.RefreshToken != "" {
		if err := refresh(ctx, user, &token); err != nil {
			return err
		}
		err = m.Login(ctx, token.AccessToken)
	}
	if errors.Is(err, metadata.ErrUnauthorized) {
		if err := cache.ExpireMyAnimeListAuthData(user); err != nil {
			return err
		}
	}
	return err
}

// refresh gets a new token with the refresh token and saves it. A rejected refresh marks
// the login expired.
func refresh(ctx context.Context, user string, token *oauth2.Token) error {
	id, secret, err := cache.MyAnimeListClient()
	if err != nil {
		return fmt.Errorf("get the myanimelist client: %w", err)
	}
	fresh, err := oauth.RefreshMyAnimeList(oauthContext(ctx), id, secret, token.RefreshToken)
	if errors.Is(err, oauth.ErrRefreshRejected) {
		if err := cache.ExpireMyAnimeListAuthData(user); err != nil {
			return err
		}
	}
	if err != nil {
		return err
	}
	if fresh.RefreshToken == "" {
		fresh.RefreshToken = token.RefreshToken
	}
	if err := cache.SetMyAnimeListAuthData(user, fresh); err != nil {
		return fmt.Errorf("save the refreshed login: %w", err)
	}
	*token = *fresh
	return nil
}

// ResetLogin makes the next EnsureLogin log in again with the saved login (it changed, e.g.
// from a TUI while `mangal serve` runs).
func ResetLogin() {
	loginSem <- struct{}{}
	defer func() { <-loginSem }()
	loginTried = false
}

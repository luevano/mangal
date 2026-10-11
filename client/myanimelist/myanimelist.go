// Package myanimelist is the MyAnimeList metadata provider shared by every client, and its
// login. It needs the client id of a MyAnimeList API client (metadata.myanimelist.client_id).
package myanimelist

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
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
	// maxQuery caps the search queries, in runes.
	// TODO: drop once libmangal caps them; MyAnimeList's limit isn't documented.
	maxQuery = 64
)

// ErrNotConfigured is MyAnimeList without a client id.
var ErrNotConfigured = errors.New("not configured: set metadata.myanimelist.client_id")

var (
	mal      *myanimelist.MyAnimeList
	provider *metadata.ProviderWithCache
	malLock  sync.Mutex

	// loginSem serializes the logins, a channel so waiting can be cancelled
	loginSem   = make(chan struct{}, 1)
	loginTried bool

	loginErr     error
	loginErrLock sync.Mutex

	// httpClient is swapped in tests
	httpClient = loader.HTTPClient
)

// MyAnimeList is the MyAnimeList metadata provider shared by every client, ErrNotConfigured
// without a client id. It makes no request: EnsureLogin logs in with the saved user.
func MyAnimeList() (*metadata.ProviderWithCache, error) {
	malLock.Lock()
	defer malLock.Unlock()
	if provider != nil {
		return provider, nil
	}
	// not kept: the client id can be set later in the run
	id := config.Metadata.MyAnimeList.ClientID.Get()
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
	cacheOpts.Provider = capped{m}
	cacheOpts.CacheStore = cache.CacheStore
	p, err := metadata.NewProviderWithCache(cacheOpts)
	if err != nil {
		return nil, fmt.Errorf("create myanimelist: %w", err)
	}
	mal, provider = m, p
	return provider, nil
}

// capped caps the search queries to maxQuery runes.
type capped struct {
	*myanimelist.MyAnimeList
}

func (c capped) Search(ctx context.Context, query string) ([]metadata.Metadata, error) {
	if r := []rune(query); len(r) > maxQuery {
		query = strings.TrimSpace(string(r[:maxQuery]))
	}
	return c.MyAnimeList.Search(ctx, query)
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
	fresh, err := oauth.RefreshMyAnimeList(oauthContext(ctx), config.Metadata.MyAnimeList.ClientID.Get(), config.Metadata.MyAnimeList.ClientSecret.Get(), token.RefreshToken)
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

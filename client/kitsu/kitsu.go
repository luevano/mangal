// Package kitsu is the Kitsu metadata provider shared by every client, and its login.
package kitsu

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/libmangal/metadata/kitsu"
	"github.com/luevano/mangal/v5/client/internal/logins"
	mlog "github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/provider/loader"
	"github.com/luevano/mangal/v5/util/cache"
)

// refreshBefore is how long before the token expires it's refreshed.
const refreshBefore = 24 * time.Hour

var (
	kitsu_    *kitsu.Kitsu
	provider  *metadata.ProviderWithCache
	kitsuErr  error
	kitsuOnce sync.Once

	account *logins.Account

	// httpClient is swapped in tests
	httpClient = loader.HTTPClient
)

func init() {
	account = logins.New(logins.Service{
		Name:         "kitsu",
		History:      cache.KitsuAuthHistory,
		LoggedOut:    cache.KitsuLoggedOut,
		SetLoggedOut: cache.SetKitsuLoggedOut,
		DeleteToken:  cache.DeleteKitsuAuthData,
		Check:        checkLogin,
		Drop: func() error {
			k, err := client()
			if err != nil || !k.Authenticated() {
				return err
			}
			return k.Logout()
		},
	})
}

// Kitsu is the Kitsu metadata provider shared by every client. It makes no request:
// EnsureLogin logs in with the saved user.
func Kitsu() (*metadata.ProviderWithCache, error) {
	kitsuOnce.Do(func() {
		provider, kitsuErr = newKitsu()
	})
	return provider, kitsuErr
}

func newKitsu() (*metadata.ProviderWithCache, error) {
	opts := kitsu.DefaultOptions()
	opts.HTTPClient = httpClient()

	k, err := kitsu.NewKitsu(opts)
	if err != nil {
		return nil, fmt.Errorf("create kitsu: %w", err)
	}

	cacheOpts := metadata.DefaultProviderWithCacheOptions()
	cacheOpts.Provider = k
	cacheOpts.CacheStore = cache.CacheStore

	p, err := metadata.NewProviderWithCache(cacheOpts)
	if err != nil {
		return nil, fmt.Errorf("create kitsu: %w", err)
	}
	// logged before any client sets its logger too (e.g. library commands)
	p.Logger().SetOnLog(mlog.ProviderHook("kitsu"))
	// the login methods are only on the concrete client
	kitsu_ = k
	return p, nil
}

func client() (*kitsu.Kitsu, error) {
	if _, err := Kitsu(); err != nil {
		return nil, err
	}
	return kitsu_, nil
}

// EnsureLogin logs into Kitsu with the saved user once per process (see CheckLogin).
// A cancelled attempt is tried again by the next call.
func EnsureLogin(ctx context.Context) error {
	return account.Ensure(ctx)
}

// Login is the saved login of the last Kitsu user.
type Login struct {
	User    string
	Expires time.Time
	// Err is why the login couldn't be checked this run (e.g. offline).
	Err   error
	token kitsu.Token
}

// Expired is true once the token expired and can't be refreshed (Kitsu rejected it).
func (l Login) Expired() bool {
	return l.token.RefreshToken == "" && !l.Expires.IsZero() && time.Now().After(l.Expires)
}

// SavedLogin is the last user's saved login, read locally (no request). There's none after
// a logout, until the next login.
func SavedLogin() (Login, bool, error) {
	user, err := account.LastUser()
	if err != nil || user == "" {
		return Login{}, false, err
	}
	var token kitsu.Token
	found, err := cache.GetKitsuAuthData(user, &token)
	if err != nil || !found {
		return Login{}, false, err
	}
	return Login{User: user, Expires: token.Expiry(), Err: account.Err(), token: token}, true, nil
}

// CheckLogin logs into Kitsu with the saved login, unless it already is or the login
// expired. The token is refreshed when it expires soon, one Kitsu rejects is marked expired.
func CheckLogin(ctx context.Context) error {
	return account.CheckLogin(ctx)
}

func checkLogin(ctx context.Context) error {
	k, err := client()
	if err != nil {
		return err
	}
	if k.Authenticated() {
		return nil
	}
	login, found, err := SavedLogin()
	if err != nil || !found || login.Expired() {
		return err
	}

	err = logInSaved(ctx, k, login.User, login.token)
	account.SetErr(ctx, err, errors.Is(err, metadata.ErrUnauthorized))
	if err != nil {
		return fmt.Errorf("log in %q: %w", login.User, err)
	}
	return nil
}

// logInSaved logs in with the saved token of the user, refreshed first when it expires
// soon, or when Kitsu rejects it. A rejected refresh marks the login expired.
func logInSaved(ctx context.Context, k *kitsu.Kitsu, user string, token kitsu.Token) error {
	expiry := token.Expiry()
	refreshed := false
	if token.RefreshToken != "" && !expiry.IsZero() && time.Until(expiry) < refreshBefore {
		if err := refresh(ctx, k, user, &token); err != nil {
			// offline: the token may still be good
			if errors.Is(err, metadata.ErrUnauthorized) || time.Now().After(expiry) {
				return err
			}
		} else {
			refreshed = true
		}
	}

	err := k.Login(ctx, token.AccessToken)
	if errors.Is(err, metadata.ErrUnauthorized) && !refreshed && token.RefreshToken != "" {
		if err := refresh(ctx, k, user, &token); err != nil {
			return err
		}
		err = k.Login(ctx, token.AccessToken)
	}
	if errors.Is(err, metadata.ErrUnauthorized) {
		if err := cache.ExpireKitsuAuthData(user); err != nil {
			return err
		}
	}
	return err
}

// refresh gets a new token with the refresh token and saves it. A rejected refresh marks
// the login expired.
func refresh(ctx context.Context, k *kitsu.Kitsu, user string, token *kitsu.Token) error {
	fresh, err := k.RefreshToken(ctx, token.RefreshToken)
	if errors.Is(err, metadata.ErrUnauthorized) {
		if err := cache.ExpireKitsuAuthData(user); err != nil {
			return err
		}
	}
	if err != nil {
		return err
	}
	// a refresh may not hand out a new refresh token
	if fresh.RefreshToken == "" {
		fresh.RefreshToken = token.RefreshToken
	}
	if err := cache.SetKitsuAuthData(user, fresh); err != nil {
		return fmt.Errorf("save the refreshed login: %w", err)
	}
	*token = fresh
	return nil
}

// ResetLogin is ResetLoginContext with context.Background.
func ResetLogin() {
	if err := ResetLoginContext(context.Background()); err != nil {
		mlog.Warn("kitsu logout: %s", err)
	}
}

// ResetLoginContext logs out of this process's session and makes the next EnsureLogin log
// in again with the saved login (it changed, e.g. from a TUI while `mangal serve` runs).
func ResetLoginContext(ctx context.Context) error {
	return account.Reset(ctx)
}

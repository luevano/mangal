package anilist

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/libmangal/metadata/anilist"
	mlog "github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/provider/loader"
	"github.com/luevano/mangal/v5/util/cache"
	"golang.org/x/oauth2"
)

// expiringSoon is how long before the login expires the notice starts.
const expiringSoon = 7 * 24 * time.Hour

var (
	anilist_    *metadata.ProviderWithCache
	anilistErr  error
	anilistOnce sync.Once

	// loginSem serializes the logins, a channel so waiting can be cancelled
	loginSem   = make(chan struct{}, 1)
	loginTried bool

	loginErr     error
	loginErrLock sync.Mutex

	// httpClient is swapped in tests
	httpClient = loader.HTTPClient
)

// Anilist is the AniList metadata provider shared by every client. It makes no request:
// EnsureLogin logs in with the saved user.
func Anilist() (*metadata.ProviderWithCache, error) {
	anilistOnce.Do(func() {
		anilist_, anilistErr = newAnilist()
	})
	return anilist_, anilistErr
}

func newAnilist() (*metadata.ProviderWithCache, error) {
	aniOpts := anilist.DefaultOptions()
	aniOpts.HTTPClient = httpClient()

	ani, err := anilist.NewAnilist(aniOpts)
	if err != nil {
		return nil, fmt.Errorf("create anilist: %w", err)
	}

	opts := metadata.DefaultProviderWithCacheOptions()
	opts.Provider = ani
	opts.CacheStore = cache.CacheStore

	provider, err := metadata.NewProviderWithCache(opts)
	if err != nil {
		return nil, fmt.Errorf("create anilist: %w", err)
	}
	return provider, nil
}

// EnsureLogin logs into AniList with the saved user once per process (see CheckLogin).
// A cancelled attempt is tried again by the next call.
func EnsureLogin(ctx context.Context) error {
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
		mlog.Warn("anilist login: %s", err)
	}
	return err
}

// Login is the saved login of the last Anilist user.
type Login struct {
	User    string
	Expires time.Time
	// Err is why the login couldn't be checked this run (e.g. offline).
	Err   error
	token oauth2.Token
}

// Expired is true once the token's expiry passed, or Anilist rejected it (marked expired then).
func (l Login) Expired() bool {
	return !l.Expires.IsZero() && time.Now().After(l.Expires)
}

// SavedLogin is the last user's saved login, read locally (no request). There's none after
// a logout, until the next login.
func SavedLogin() (Login, bool, error) {
	if out, err := cache.AnilistLoggedOut(); err != nil || out {
		return Login{}, false, err
	}
	user, err := lastUser()
	if err != nil || user == "" {
		return Login{}, false, err
	}
	var token oauth2.Token
	found, err := cache.GetAnilistAuthData(user, &token)
	if err != nil || !found {
		return Login{}, false, err
	}
	loginErrLock.Lock()
	defer loginErrLock.Unlock()
	return Login{User: user, Expires: token.Expiry, Err: loginErr, token: token}, true, nil
}

// CheckLogin logs into Anilist with the saved login, unless it already is or the login
// expired. A token Anilist rejects is marked expired.
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
	ani, err := Anilist()
	if err != nil {
		return err
	}
	if ani.Authenticated() {
		return nil
	}
	login, found, err := SavedLogin()
	if err != nil || !found || login.Expired() {
		return err
	}

	err = ani.LoginContext(ctx, login.token.AccessToken)
	unauthorized := errors.Is(err, metadata.ErrUnauthorized)
	loginErrLock.Lock()
	loginErr = nil
	if err != nil && !unauthorized && ctx.Err() == nil {
		loginErr = err
	}
	loginErrLock.Unlock()
	if unauthorized {
		if err := cache.ExpireAnilistAuthData(login.User); err != nil {
			return err
		}
	}
	if err != nil {
		return fmt.Errorf("log in %q: %w", login.User, err)
	}
	return nil
}

// LoginNotice tells when the saved login expired or expires soon, empty otherwise.
// It's read locally: a token Anilist rejects shows up from the next run.
func LoginNotice() string {
	login, found, err := SavedLogin()
	if err != nil || !found || login.Expires.IsZero() {
		return ""
	}
	how := `log in again from the accounts (a in the TUI home, then l on Anilist) or "mangal accounts"; forgetting the user there stops this notice`
	switch {
	case login.Expired():
		return fmt.Sprintf("The Anilist login of %q expired on %s, %s.", login.User, login.Expires.Format(time.DateOnly), how)
	case time.Until(login.Expires) < expiringSoon:
		return fmt.Sprintf("The Anilist login of %q expires on %s, %s.", login.User, login.Expires.Format(time.DateOnly), how)
	}
	return ""
}

// ResetLogin makes the next EnsureLogin log in again with the saved login (it changed, e.g.
// from a TUI while `mangal serve` runs).
func ResetLogin() {
	loginSem <- struct{}{}
	defer func() { <-loginSem }()
	loginTried = false
}

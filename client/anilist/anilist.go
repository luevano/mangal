package anilist

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/libmangal/metadata/anilist"
	mlog "github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/util/cache"
	"golang.org/x/oauth2"
)

// TODO: handle the errors instead of just fatal'ing

// expiringSoon is how long before the login expires the notice starts.
const expiringSoon = 7 * 24 * time.Hour

var (
	anilist_    *metadata.ProviderWithCache
	anilistOnce sync.Once

	loginErr     error
	loginErrLock sync.Mutex
)

func Anilist() *metadata.ProviderWithCache {
	anilistOnce.Do(func() {
		anilist_ = newAnilist()
		if err := CheckLogin(context.Background()); err != nil {
			mlog.Log("anilist login: %s", err)
		}
	})
	return anilist_
}

func newAnilist() *metadata.ProviderWithCache {
	aniOpts := anilist.DefaultOptions()

	ani, err := anilist.NewAnilist(aniOpts)
	if err != nil {
		log.Fatal(err)
	}

	opts := metadata.DefaultProviderWithCacheOptions()
	opts.Provider = ani
	opts.CacheStore = cache.CacheStore

	provider, err := metadata.NewProviderWithCache(opts)
	if err != nil {
		log.Fatal(err)
	}
	return provider
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

// SavedLogin is the last user's saved login, read locally (no request).
func SavedLogin() (Login, bool, error) {
	var history cache.UserHistory
	if _, err := cache.GetAuthHistory(cache.AnilistAuthHistory, &history); err != nil {
		return Login{}, false, err
	}
	user := history.Last()
	if user == "" {
		return Login{}, false, nil
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
	if anilist_.Authenticated() {
		return nil
	}
	login, found, err := SavedLogin()
	if err != nil || !found || login.Expired() {
		return err
	}

	err = anilist_.LoginContext(ctx, login.token.AccessToken)
	unauthorized := errors.Is(err, metadata.ErrUnauthorized)
	loginErrLock.Lock()
	loginErr = nil
	if err != nil && !unauthorized {
		loginErr = err
	}
	loginErrLock.Unlock()
	if unauthorized {
		if err := cache.ExpireAnilistAuthData(login.User); err != nil {
			return err
		}
	}
	return err
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

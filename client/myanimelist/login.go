package myanimelist

import (
	"context"
	"fmt"
	"time"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/libmangal/metadata/myanimelist"
	"github.com/luevano/mangal/v5/auth/oauth"
	"github.com/luevano/mangal/v5/client/internal/logins"
	"github.com/luevano/mangal/v5/util/cache"
	"golang.org/x/oauth2"
)

// ErrUserNotFound is a user of the history without a saved login (it's removed from the history).
var ErrUserNotFound = logins.ErrUserNotFound

// ExpiredError is a saved login that expired, or MyAnimeList rejected.
type ExpiredError = logins.ExpiredError

// Users are the users with a saved login, the last one logged in last.
func Users() ([]string, error) {
	return account.Users()
}

// withLogin runs the login holding the login lock, so EnsureLogin doesn't race it.
func withLogin(ctx context.Context, login func(*myanimelist.MyAnimeList) error) (metadata.User, error) {
	m, err := client()
	if err != nil {
		return nil, err
	}
	return account.Login(ctx, func() (metadata.User, error) {
		if err := login(m); err != nil {
			return nil, err
		}
		return m.User(), nil
	})
}

// LoginNew authorizes mangal with the configured MyAnimeList API client in the browser,
// logs in and saves the login.
func LoginNew(ctx context.Context) (metadata.User, error) {
	return loginNew(ctx, func(o *oauth.MyAnimeListLoginOption) error {
		return o.Authorize(oauthContext(ctx))
	})
}

// LoginManual is LoginNew showing the authorize url instead of opening it, the redirected
// address (or code) can be pasted, see oauth.Manual.
func LoginManual(ctx context.Context, manual oauth.Manual) (metadata.User, error) {
	return loginNew(ctx, func(o *oauth.MyAnimeListLoginOption) error {
		return o.AuthorizeManual(oauthContext(ctx), manual)
	})
}

func loginNew(ctx context.Context, authorize func(*oauth.MyAnimeListLoginOption) error) (metadata.User, error) {
	if _, err := MyAnimeList(); err != nil {
		return nil, err
	}
	id, secret, err := cache.MyAnimeListClient()
	if err != nil {
		return nil, fmt.Errorf("get the myanimelist client: %w", err)
	}
	option, err := oauth.NewMyAnimeListLoginOption(id, secret)
	if err != nil {
		return nil, err
	}
	if err := authorize(option); err != nil {
		return nil, fmt.Errorf("authorize: %w", err)
	}
	token := option.Token()

	u, err := withLogin(ctx, func(m *myanimelist.MyAnimeList) error {
		return m.Login(ctx, token.AccessToken)
	})
	if err != nil {
		return nil, fmt.Errorf("log in: %w", err)
	}
	if err := cache.SetMyAnimeListAuthData(u.Name(), token); err != nil {
		return nil, fmt.Errorf("save login of %q: %w", u.Name(), err)
	}
	return u, account.LoggedIn(u)
}

// LoginSaved logs in with the saved login of the user, refreshing it if needed. It returns
// ErrUserNotFound or an *ExpiredError when it can't.
func LoginSaved(ctx context.Context, user string) (metadata.User, error) {
	var token oauth2.Token
	found, err := cache.GetMyAnimeListAuthData(user, &token)
	if err != nil {
		return nil, fmt.Errorf("get login of %q: %w", user, err)
	}
	if !found {
		return nil, account.NotFound(user)
	}
	if (Login{Expires: token.Expiry, token: token}).Expired() {
		return nil, &ExpiredError{User: user, On: token.Expiry}
	}

	u, err := withLogin(ctx, func(m *myanimelist.MyAnimeList) error {
		return logInSaved(ctx, m, user, token)
	})
	if rejected(err) {
		return nil, &ExpiredError{User: user, On: time.Now()}
	}
	if err != nil {
		return nil, fmt.Errorf("log in %q: %w", user, err)
	}
	return u, account.LoggedIn(u)
}

// Logout is LogoutContext with context.Background.
func Logout() error {
	return LogoutContext(context.Background())
}

// LogoutContext logs out, also of the next runs: the saved login stays (a login picks it
// from the saved users) but isn't used on startup until the next login.
func LogoutContext(ctx context.Context) error {
	return account.Logout(ctx)
}

// Forget deletes the saved login of the user.
func Forget(user string) error {
	return account.Forget(user)
}

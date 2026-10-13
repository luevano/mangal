package kitsu

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/libmangal/metadata/kitsu"
	"github.com/luevano/mangal/v5/client/internal/logins"
	"github.com/luevano/mangal/v5/util/cache"
)

// ErrUserNotFound is a user of the history without a saved login (it's removed from the history).
var ErrUserNotFound = logins.ErrUserNotFound

// ExpiredError is a saved login that expired, or Kitsu rejected.
type ExpiredError = logins.ExpiredError

// Users are the users with a saved login, the last one logged in last.
func Users() ([]string, error) {
	return account.Users()
}

// withLogin runs the login holding the login lock, so EnsureLogin doesn't race it.
func withLogin(ctx context.Context, login func(*kitsu.Kitsu) error) (metadata.User, error) {
	k, err := client()
	if err != nil {
		return nil, err
	}
	return account.Login(ctx, func() (metadata.User, error) {
		if err := login(k); err != nil {
			return nil, err
		}
		return k.User(), nil
	})
}

// LoginPassword logs in with the user's email (or name) and password and saves the token,
// the password isn't kept. A wrong password matches metadata.ErrUnauthorized.
func LoginPassword(ctx context.Context, username, password string) (metadata.User, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, errors.New("empty username or password")
	}
	var token kitsu.Token
	u, err := withLogin(ctx, func(k *kitsu.Kitsu) error {
		var err error
		token, err = k.LoginPassword(ctx, username, password)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("log in: %w", err)
	}
	if err := cache.SetKitsuAuthData(u.Name(), token); err != nil {
		return nil, fmt.Errorf("save login of %q: %w", u.Name(), err)
	}
	return u, account.LoggedIn(u)
}

// LoginSaved logs in with the saved login of the user, refreshing it if needed. It returns
// ErrUserNotFound or an *ExpiredError when it can't.
func LoginSaved(ctx context.Context, user string) (metadata.User, error) {
	var token kitsu.Token
	found, err := cache.GetKitsuAuthData(user, &token)
	if err != nil {
		return nil, fmt.Errorf("get login of %q: %w", user, err)
	}
	if !found {
		return nil, account.NotFound(user)
	}
	if (Login{Expires: token.Expiry(), token: token}).Expired() {
		return nil, &ExpiredError{User: user, On: token.Expiry()}
	}

	u, err := withLogin(ctx, func(k *kitsu.Kitsu) error {
		return logInSaved(ctx, k, user, token)
	})
	if errors.Is(err, metadata.ErrUnauthorized) {
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

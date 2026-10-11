package kitsu

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/libmangal/metadata/kitsu"
	"github.com/luevano/mangal/v5/util/cache"
)

// ErrUserNotFound is a user of the history without a saved login (it's removed from the history).
var ErrUserNotFound = errors.New("saved login not found")

// ExpiredError is a saved login that expired, or Kitsu rejected.
type ExpiredError struct {
	User string
	On   time.Time
}

func (e *ExpiredError) Error() string {
	return fmt.Sprintf("the login of %q expired on %s, log in again", e.User, e.On.Format(time.DateOnly))
}

// Users are the users with a saved login, the last one logged in last.
func Users() ([]string, error) {
	var history cache.UserHistory
	if _, err := cache.GetAuthHistory(cache.KitsuAuthHistory, &history); err != nil {
		return nil, fmt.Errorf("get kitsu users: %w", err)
	}
	return history.Get(), nil
}

func lastUser() (string, error) {
	users, err := Users()
	if err != nil || len(users) == 0 {
		return "", err
	}
	return users[len(users)-1], nil
}

func updateUsers(update func(*cache.UserHistory)) error {
	var history cache.UserHistory
	if _, err := cache.GetAuthHistory(cache.KitsuAuthHistory, &history); err != nil {
		return fmt.Errorf("get kitsu users: %w", err)
	}
	update(&history)
	if err := cache.SetAuthHistory(cache.KitsuAuthHistory, history); err != nil {
		return fmt.Errorf("set kitsu users: %w", err)
	}
	return nil
}

// withLogin runs the login holding the login lock, so EnsureLogin doesn't race it.
func withLogin(ctx context.Context, login func(*kitsu.Kitsu) error) (metadata.User, error) {
	k, err := client()
	if err != nil {
		return nil, err
	}
	select {
	case loginSem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-loginSem }()
	if err := login(k); err != nil {
		return nil, err
	}
	loginTried = true
	user := k.User()
	if user == nil {
		return nil, errors.New("kitsu returned no user")
	}
	return user, nil
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
	return u, loggedIn(u)
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
		if err := updateUsers(func(h *cache.UserHistory) { h.Delete(user) }); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%q: %w", user, ErrUserNotFound)
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
	return u, loggedIn(u)
}

// loggedIn makes the user the last one, so it logs in on startup again.
func loggedIn(u metadata.User) error {
	if err := updateUsers(func(h *cache.UserHistory) { h.Add(u.Name()) }); err != nil {
		return err
	}
	if err := cache.SetKitsuLoggedOut(false); err != nil {
		return fmt.Errorf("save the login: %w", err)
	}
	return nil
}

// Logout logs out, also of the next runs: the saved login stays (a login picks it from the
// saved users) but isn't used on startup until the next login.
func Logout() error {
	k, err := client()
	if err != nil {
		return err
	}
	loginSem <- struct{}{}
	defer func() { <-loginSem }()
	loginTried = true
	if err := cache.SetKitsuLoggedOut(true); err != nil {
		return fmt.Errorf("save the logout: %w", err)
	}
	if !k.Authenticated() {
		return nil
	}
	return k.Logout()
}

// Forget deletes the saved login of the user.
func Forget(user string) error {
	if err := cache.DeleteKitsuAuthData(user); err != nil {
		return fmt.Errorf("delete login of %q: %w", user, err)
	}
	return updateUsers(func(h *cache.UserHistory) { h.Delete(user) })
}

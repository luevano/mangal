package myanimelist

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/libmangal/metadata/myanimelist"
	"github.com/luevano/mangal/v5/auth/oauth"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/util/cache"
	"golang.org/x/oauth2"
)

// ErrUserNotFound is a user of the history without a saved login (it's removed from the history).
var ErrUserNotFound = errors.New("saved login not found")

// ExpiredError is a saved login that expired, or MyAnimeList rejected.
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
	if _, err := cache.GetAuthHistory(cache.MyAnimeListAuthHistory, &history); err != nil {
		return nil, fmt.Errorf("get myanimelist users: %w", err)
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
	if _, err := cache.GetAuthHistory(cache.MyAnimeListAuthHistory, &history); err != nil {
		return fmt.Errorf("get myanimelist users: %w", err)
	}
	update(&history)
	if err := cache.SetAuthHistory(cache.MyAnimeListAuthHistory, history); err != nil {
		return fmt.Errorf("set myanimelist users: %w", err)
	}
	return nil
}

// withLogin runs the login holding the login lock, so EnsureLogin doesn't race it.
func withLogin(ctx context.Context, login func(*myanimelist.MyAnimeList) error) (metadata.User, error) {
	m, err := client()
	if err != nil {
		return nil, err
	}
	select {
	case loginSem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-loginSem }()
	if err := login(m); err != nil {
		return nil, err
	}
	loginTried = true
	user := m.User()
	if user == nil {
		return nil, errors.New("myanimelist returned no user")
	}
	return user, nil
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
	option, err := oauth.NewMyAnimeListLoginOption(config.MyAnimeList.ClientID.Get(), config.MyAnimeList.ClientSecret.Get())
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
	return u, loggedIn(u)
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
		if err := updateUsers(func(h *cache.UserHistory) { h.Delete(user) }); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%q: %w", user, ErrUserNotFound)
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
	return u, loggedIn(u)
}

// loggedIn makes the user the last one, so it logs in on startup again.
func loggedIn(u metadata.User) error {
	if err := updateUsers(func(h *cache.UserHistory) { h.Add(u.Name()) }); err != nil {
		return err
	}
	if err := cache.SetMyAnimeListLoggedOut(false); err != nil {
		return fmt.Errorf("save the login: %w", err)
	}
	return nil
}

// Logout logs out, also of the next runs: the saved login stays (a login picks it from the
// saved users) but isn't used on startup until the next login.
func Logout() error {
	loginSem <- struct{}{}
	defer func() { <-loginSem }()
	loginTried = true
	if err := cache.SetMyAnimeListLoggedOut(true); err != nil {
		return fmt.Errorf("save the logout: %w", err)
	}
	m, err := client()
	if errors.Is(err, ErrNotConfigured) || err == nil && !m.Authenticated() {
		return nil
	}
	if err != nil {
		return err
	}
	return m.Logout()
}

// Forget deletes the saved login of the user.
func Forget(user string) error {
	if err := cache.DeleteMyAnimeListAuthData(user); err != nil {
		return fmt.Errorf("delete login of %q: %w", user, err)
	}
	return updateUsers(func(h *cache.UserHistory) { h.Delete(user) })
}

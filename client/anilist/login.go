package anilist

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/auth/oauth"
	"github.com/luevano/mangal/v5/util/cache"
	"golang.org/x/oauth2"
)

// ErrUserNotFound is a user of the history without a saved login (it's removed from the history).
var ErrUserNotFound = errors.New("saved login not found")

// ExpiredError is a saved login that expired, or AniList rejected.
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
	if _, err := cache.GetAuthHistory(cache.AnilistAuthHistory, &history); err != nil {
		return nil, fmt.Errorf("get anilist users: %w", err)
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
	if _, err := cache.GetAuthHistory(cache.AnilistAuthHistory, &history); err != nil {
		return fmt.Errorf("get anilist users: %w", err)
	}
	update(&history)
	if err := cache.SetAuthHistory(cache.AnilistAuthHistory, history); err != nil {
		return fmt.Errorf("set anilist users: %w", err)
	}
	return nil
}

// login logs in with the token, holding the login lock so EnsureLogin doesn't race it.
func login(ctx context.Context, token string) (metadata.User, error) {
	ani, err := Anilist()
	if err != nil {
		return nil, err
	}
	select {
	case loginSem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-loginSem }()
	if err := ani.LoginContext(ctx, token); err != nil {
		return nil, err
	}
	loginTried = true
	user := ani.User()
	if user == nil {
		return nil, errors.New("anilist returned no user")
	}
	return user, nil
}

// LoginSaved logs in with the saved login of the user. It returns ErrUserNotFound or an
// *ExpiredError when it can't.
func LoginSaved(ctx context.Context, user string) (metadata.User, error) {
	var token oauth2.Token
	found, err := cache.GetAnilistAuthData(user, &token)
	if err != nil {
		return nil, fmt.Errorf("get login of %q: %w", user, err)
	}
	if !found {
		if err := updateUsers(func(h *cache.UserHistory) { h.Delete(user) }); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%q: %w", user, ErrUserNotFound)
	}
	if !token.Expiry.IsZero() && time.Now().After(token.Expiry) {
		return nil, &ExpiredError{User: user, On: token.Expiry}
	}

	u, err := login(ctx, token.AccessToken)
	if errors.Is(err, metadata.ErrUnauthorized) {
		if err := cache.ExpireAnilistAuthData(user); err != nil {
			return nil, err
		}
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
	if err := cache.SetAnilistLoggedOut(false); err != nil {
		return fmt.Errorf("save the login: %w", err)
	}
	return nil
}

// LoginNew authorizes mangal with the AniList API client (in the browser), logs in and
// saves the login. An empty secret is an implicit grant.
func LoginNew(ctx context.Context, id, secret string) (metadata.User, error) {
	id, secret = clean(id), clean(secret)
	if id == "" {
		return nil, errors.New("empty client id")
	}
	option, err := oauth.NewAnilistLoginOption(id, secret)
	if err != nil {
		return nil, err
	}
	if err := option.Authorize(ctx); err != nil {
		return nil, fmt.Errorf("authorize: %w", err)
	}
	token := option.Token()

	u, err := login(ctx, token.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("log in: %w", err)
	}
	if err := cache.SetAnilistAuthData(u.Name(), token); err != nil {
		return nil, fmt.Errorf("save login of %q: %w", u.Name(), err)
	}
	return u, loggedIn(u)
}

// Logout logs out, also of the next runs: the saved login stays (a login picks it from the
// saved users) but isn't used on startup until the next login.
func Logout() error {
	ani, err := Anilist()
	if err != nil {
		return err
	}
	loginSem <- struct{}{}
	defer func() { <-loginSem }()
	loginTried = true
	if err := cache.SetAnilistLoggedOut(true); err != nil {
		return fmt.Errorf("save the logout: %w", err)
	}
	if !ani.Authenticated() {
		return nil
	}
	return ani.Logout()
}

// Forget deletes the saved login of the user.
func Forget(user string) error {
	if err := cache.DeleteAnilistAuthData(user); err != nil {
		return fmt.Errorf("delete login of %q: %w", user, err)
	}
	return updateUsers(func(h *cache.UserHistory) { h.Delete(user) })
}

// clean drops every space, pasted ids and secrets often carry some.
func clean(in string) string {
	return strings.Join(strings.Fields(in), "")
}

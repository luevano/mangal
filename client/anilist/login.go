package anilist

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/auth/oauth"
	"github.com/luevano/mangal/v5/client/internal/logins"
	"github.com/luevano/mangal/v5/config"
	mlog "github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/util/cache"
	"golang.org/x/oauth2"
)

// ErrUserNotFound is a user of the history without a saved login (it's removed from the history).
var ErrUserNotFound = logins.ErrUserNotFound

// ExpiredError is a saved login that expired, or AniList rejected.
type ExpiredError = logins.ExpiredError

// Users are the users with a saved login, the last one logged in last.
func Users() ([]string, error) {
	return account.Users()
}

// login logs in with the token, holding the login lock so EnsureLogin doesn't race it.
func login(ctx context.Context, token string) (metadata.User, error) {
	ani, err := Anilist()
	if err != nil {
		return nil, err
	}
	return account.Login(ctx, func() (metadata.User, error) {
		if err := ani.LoginContext(ctx, token); err != nil {
			return nil, err
		}
		return ani.User(), nil
	})
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
		return nil, account.NotFound(user)
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
	return u, account.LoggedIn(u)
}

// Pin is the config's pin login (metadata.anilist.redirect), see oauth.AnilistLoginOption.Pin.
func Pin() bool {
	return config.Metadata.Anilist.Redirect.Get() == "pin"
}

// LoginNew authorizes mangal with the AniList API client (in the browser), logs in and
// saves the login. An empty secret is an implicit grant. Not with the pin login, which
// needs a paste (LoginManual).
func LoginNew(ctx context.Context, id, secret string) (metadata.User, error) {
	if Pin() {
		return nil, fmt.Errorf("metadata.anilist.redirect is pin, log in with mangal anilist login: %w", oauth.ErrNeedsPaste)
	}
	return loginNew(ctx, id, secret, false, func(o *oauth.AnilistLoginOption) error {
		return o.Authorize(ctx)
	})
}

// LoginManual is LoginNew showing the authorize url instead of opening it, the redirected
// address (or code, or token) can be pasted, see oauth.Manual. With pin AniList's page
// shows the code to paste (the client must have its redirect URL).
func LoginManual(ctx context.Context, id, secret string, pin bool, manual oauth.Manual) (metadata.User, error) {
	return loginNew(ctx, id, secret, pin, func(o *oauth.AnilistLoginOption) error {
		return o.AuthorizeManual(ctx, manual)
	})
}

func loginNew(ctx context.Context, id, secret string, pin bool, authorize func(*oauth.AnilistLoginOption) error) (metadata.User, error) {
	id, secret = clean(id), clean(secret)
	if id == "" {
		return nil, errors.New("empty client id")
	}
	option, err := oauth.NewAnilistLoginOption(id, secret)
	if err != nil {
		return nil, err
	}
	option.Pin = pin
	if err := authorize(option); err != nil {
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
	// kept for the next new login
	if err := cache.SetAnilistClient(id, secret); err != nil {
		mlog.Warn("save the anilist client: %s", err)
	}
	return u, account.LoggedIn(u)
}

// Client is the AniList API client of the last new login, an empty id if none.
func Client() (id, secret string, err error) {
	return cache.AnilistClient()
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

// clean drops every space, pasted ids and secrets often carry some.
func clean(in string) string {
	return strings.Join(strings.Fields(in), "")
}

// LoginFor is LoginManual with the saved API client for the mangal user: the local user's
// is this process' login, another's an instance of its own (ForAccount).
func LoginFor(ctx context.Context, user string, manual oauth.Manual) (metadata.User, error) {
	id, secret, err := Client()
	if err != nil {
		return nil, err
	}
	if id == "" {
		return nil, errors.New("no AniList API client yet: log in once from mangal on the server (mangal anilist login)")
	}
	if local, err := cache.LocalUser(); err != nil || local == user {
		if err != nil {
			return nil, err
		}
		return LoginManual(ctx, id, secret, Pin(), manual)
	}
	option, err := oauth.NewAnilistLoginOption(id, secret)
	if err != nil {
		return nil, err
	}
	option.Pin = Pin()
	if err := option.AuthorizeManual(ctx, manual); err != nil {
		return nil, fmt.Errorf("authorize: %w", err)
	}
	token := option.Token()
	p, err := newAnilist()
	if err != nil {
		return nil, err
	}
	if err := p.LoginContext(ctx, token.AccessToken); err != nil {
		return nil, fmt.Errorf("log in: %w", err)
	}
	u := p.User()
	if err := cache.SetAnilistAuthData(u.Name(), token); err != nil {
		return nil, fmt.Errorf("save login of %q: %w", u.Name(), err)
	}
	accounts.Store(u.Name(), p)
	return u, cache.AddAuthAccount(user, cache.AnilistAuthHistory, u.Name())
}

// LogoutFor logs the mangal user out of AniList (the local user: this process too).
func LogoutFor(ctx context.Context, user string) error {
	if local, err := cache.LocalUser(); err != nil || local == user {
		if err != nil {
			return err
		}
		return LogoutContext(ctx)
	}
	return cache.SetAuthLoggedOut(user, cache.AnilistAuthHistory, true)
}

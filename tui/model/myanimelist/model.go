// Package myanimelist is the MyAnimeList login screen (mangal myanimelist, and from the
// accounts).
package myanimelist

import (
	"context"
	"errors"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/auth/oauth"
	"github.com/luevano/mangal/v5/client/myanimelist"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/login"
)

// New is the MyAnimeList login screen.
func New() base.State { return login.New(service) }

var service = login.Service{
	ID:       "myanimelist",
	Name:     "MyAnimeList",
	Color:    color.MyAnimeList,
	Check:    check,
	Status:   status,
	FormHint: "the login opens in the browser, for one on another machine use mangal myanimelist login",
	FormView: func(key string) string {
		return key + " opens the MyAnimeList login in the browser (" + oauth.MyAnimeListServerLoginURL + ")"
	},
	LoginLabel: "Waiting for the login in the browser",
	LoginNew: func(ctx context.Context, _ []string) (metadata.User, error) {
		return myanimelist.LoginNew(ctx)
	},
	LoginSaved: func(ctx context.Context, user string) error {
		_, err := myanimelist.LoginSaved(ctx, user)
		return err
	},
	NotFound: myanimelist.ErrUserNotFound,
	Expired:  login.ExpiredAs[*myanimelist.ExpiredError],
	Logout:   myanimelist.Logout,
	Forget:   myanimelist.Forget,
	Client: &login.Client{
		Fields: []login.Input{
			{Label: "Client ID", RequiredUnsaved: true},
			{Label: "Client secret", Placeholder: `(only for App Types other than "other")`, Secret: true},
		},
		Intro: []string{
			"MyAnimeList needs an API client of your own: register one at",
			"https://myanimelist.net/apiconfig (App Type \"other\", App Redirect URL",
			oauth.MyAnimeListServerCallbackURL + "), the client ID is on its page.",
		},
		Hint:      "the MyAnimeList API client mangal logs in with, saved with the logins",
		HintSaved: "the MyAnimeList API client mangal logs in with, an empty client ID removes it",
		Load: func() ([]string, error) {
			id, secret, err := myanimelist.Client()
			return []string{id, secret}, err
		},
		Save: func(_ context.Context, values []string) (string, error) {
			if err := myanimelist.SetClient(values[0], values[1]); err != nil {
				return "", err
			}
			if values[0] == "" {
				return "Removed the MyAnimeList API client", nil
			}
			return "Saved the MyAnimeList API client", nil
		},
	},
}

// check refreshes the login, without a client there's nothing to.
func check(ctx context.Context) error {
	if err := myanimelist.EnsureLogin(ctx); err != nil && !errors.Is(err, myanimelist.ErrNotConfigured) {
		return err
	}
	return nil
}

// status reads the login state, nothing is requested.
func status(context.Context) (login.Status, error) {
	m, err := myanimelist.MyAnimeList()
	if errors.Is(err, myanimelist.ErrNotConfigured) {
		return login.Status{NoClient: true}, nil
	}
	if err != nil {
		return login.Status{}, err
	}
	users, err := myanimelist.Users()
	if err != nil {
		return login.Status{}, err
	}
	var user metadata.User
	if m.Authenticated() {
		user = m.User()
	}
	return login.Status{User: user, Users: users}, nil
}

// Package anilist is the AniList login screen (mangal anilist, and from the accounts).
package anilist

import (
	"context"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/login"
)

// New is the AniList login screen.
func New() base.State { return login.New(service) }

var service = login.Service{
	ID:     "anilist",
	Name:   "Anilist",
	Color:  color.Anilist,
	Check:  anilist.EnsureLogin,
	Status: status,
	Fields: []login.Input{
		{Label: "ID", Required: true},
		{Label: "Secret", Placeholder: "(optional)", Secret: true},
	},
	FormHint:   "the id and secret of an Anilist API client, the login opens in the browser (none here? mangal anilist login)",
	LoginLabel: "Logging in, finish it in the browser",
	LoginNew: func(ctx context.Context, values []string) (metadata.User, error) {
		return anilist.LoginNew(ctx, values[0], values[1])
	},
	LoginSaved: func(ctx context.Context, user string) error {
		_, err := anilist.LoginSaved(ctx, user)
		return err
	},
	NotFound: anilist.ErrUserNotFound,
	Expired:  login.ExpiredAs[*anilist.ExpiredError],
	Logout:   anilist.Logout,
	Forget:   anilist.Forget,
}

// status reads the login state, nothing is requested.
func status(context.Context) (login.Status, error) {
	ani, err := anilist.Anilist()
	if err != nil {
		return login.Status{}, err
	}
	users, err := anilist.Users()
	if err != nil {
		return login.Status{}, err
	}
	var user metadata.User
	if ani.Authenticated() {
		user = ani.User()
	}
	id, secret, err := anilist.Client()
	if err != nil {
		return login.Status{}, err
	}
	return login.Status{User: user, Users: users, Values: []string{id, secret}}, nil
}

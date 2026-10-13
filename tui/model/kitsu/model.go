// Package kitsu is the Kitsu login screen (mangal kitsu, and from the accounts).
package kitsu

import (
	"context"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client/kitsu"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/login"
)

// New is the Kitsu login screen.
func New() base.State { return login.New(service) }

var service = login.Service{
	ID:     "kitsu",
	Name:   "Kitsu",
	Color:  color.Kitsu,
	Check:  kitsu.EnsureLogin,
	Status: status,
	Fields: []login.Input{
		{Label: "Email", Placeholder: "(or user name)", Required: true},
		// nothing keeps the password once the login is done
		{Label: "Password", Secret: true, Required: true, Clear: true},
	},
	FormHint:   "your Kitsu account, only the login token is saved, never the password",
	LoginLabel: "Logging in",
	LoginNew: func(ctx context.Context, values []string) (metadata.User, error) {
		return kitsu.LoginPassword(ctx, values[0], values[1])
	},
	Rejected: "Wrong email or password",
	LoginSaved: func(ctx context.Context, user string) error {
		_, err := kitsu.LoginSaved(ctx, user)
		return err
	},
	NotFound: kitsu.ErrUserNotFound,
	Expired:  login.ExpiredAs[*kitsu.ExpiredError],
	Logout:   kitsu.Logout,
	Forget:   kitsu.Forget,
}

// status reads the login state, nothing is requested.
func status(context.Context) (login.Status, error) {
	k, err := kitsu.Kitsu()
	if err != nil {
		return login.Status{}, err
	}
	users, err := kitsu.Users()
	if err != nil {
		return login.Status{}, err
	}
	var user metadata.User
	if k.Authenticated() {
		user = k.User()
	}
	return login.Status{User: user, Users: users}, nil
}

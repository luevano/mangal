package client

import (
	"context"
	"errors"
	"fmt"

	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/client/kitsu"
	"github.com/luevano/mangal/v5/client/myanimelist"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
)

// EnsureUsers makes sure everything recorded has a user: the user "mangal" when there's
// none, and the history and search history of before 5.28 given to the local user. The
// notice says what changed, empty if nothing did.
func EnsureUsers() (string, error) {
	st, err := cache.Storage()
	if err != nil {
		return "", err
	}
	made, err := st.Users().EnsureOne()
	if err != nil {
		return "", err
	}
	user, err := cache.LocalUser()
	if err != nil {
		return "", err
	}
	claimed, err := st.Users().ClaimHistory(user)
	if err != nil {
		return "", err
	}
	if err := cache.ClaimUserData("", user); err != nil {
		return "", err
	}
	var notice string
	if made {
		notice = fmt.Sprintf("Added the user %q, an admin without a password: mangal on this machine is them. \"mangal user passwd %s\" lets them log into the web UI, \"mangal user rename\" renames them.", storage.DefaultUser, storage.DefaultUser)
	}
	if claimed > 0 {
		notice += fmt.Sprintf(" The read history so far is %s's now.", user)
	}
	return notice, nil
}

// UseLocalUser makes the user mangal on this machine (auth.user): their history, search
// history and tracker accounts from now on, this process' tracker logins included.
func UseLocalUser(ctx context.Context, name string) error {
	st, err := cache.Storage()
	if err != nil {
		return err
	}
	u, err := st.Users().Get(name)
	if err != nil {
		return err
	}
	// a file key: made like "mangal config set" makes it
	if err := config.CreateFile(); err != nil {
		return err
	}
	if err := config.Auth.User.Write(u.Name); err != nil {
		return err
	}
	// the next login is the new user's saved account
	return errors.Join(anilist.ResetLoginContext(ctx), kitsu.ResetLoginContext(ctx), myanimelist.ResetLoginContext(ctx))
}

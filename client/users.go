package client

import (
	"fmt"

	"github.com/luevano/mangal/v5/log"
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
	if err := cache.ClaimSearchHistory("", user); err != nil {
		return "", err
	}
	var notice string
	if made {
		log.Info("added the user %q (admin, no password)", storage.DefaultUser)
		notice = fmt.Sprintf("Added the user %q, an admin without a password: mangal on this machine is them. \"mangal user passwd %s\" lets them log into the web UI, \"mangal user rename\" renames them.", storage.DefaultUser, storage.DefaultUser)
	}
	if claimed > 0 {
		log.Info("the read history so far is %q's now (%d chapters and positions)", user, claimed)
		notice += fmt.Sprintf(" The read history so far is %s's now.", user)
	}
	return notice, nil
}

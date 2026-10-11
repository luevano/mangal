package cache

import (
	"fmt"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/storage"
)

// MangaPlus secrets are kept with the other logins (auth tokens).
const (
	authOwnerMangaPlus     = "mangaplus"
	keyMangaPlusSecret     = "secret"
	keyMangaPlusFreeDevice = "free_secret"
)

func mangaPlusAuth() (storage.KV, error) {
	_, dataDB, err := open()
	return authStore(dataDB, authOwnerMangaPlus), err
}

func getString(key string) (string, error) {
	kv, err := mangaPlusAuth()
	if err != nil {
		return "", err
	}
	var value string
	_, err = kv.Get(key, &value)
	return value, err
}

func setString(key, value string) error {
	kv, err := mangaPlusAuth()
	if err != nil {
		return err
	}
	if value == "" {
		return kv.Delete(key)
	}
	return kv.Set(key, value)
}

// MangaPlusSecret is the device secret of the official app (subscription), empty if not set.
func MangaPlusSecret() (string, error) {
	return getString(keyMangaPlusSecret)
}

// SetMangaPlusSecret saves the device secret, empty removes it.
func SetMangaPlusSecret(secret string) error {
	return setString(keyMangaPlusSecret, secret)
}

// MangaPlusFreeSecret is the secret of the registered free device, empty if none yet.
func MangaPlusFreeSecret() (string, error) {
	return getString(keyMangaPlusFreeDevice)
}

// SetMangaPlusFreeSecret saves the secret of a newly registered free device.
func SetMangaPlusFreeSecret(secret string) error {
	return setString(keyMangaPlusFreeDevice, secret)
}

// keyring is true when the logins are kept in the OS keyring (auth.keyring).
var keyring bool

// useKeyring moves the logins to the OS keyring, or back to the database, as auth.keyring
// says. Never an error: the logins just stay where they are.
func useKeyring(data *storage.DB) string {
	keyring = config.Auth.Keyring.Get()
	if keyring {
		if err := storage.KeyringAvailable(); err != nil {
			keyring = false
			return fmt.Sprintf("auth.keyring is on but the OS keyring can't be used (%s), the logins stay in mangal's database.", err)
		}
	}
	moved, err := storage.MoveToKeyring(data, storage.TableAuthTokens, keyring)
	switch {
	case err != nil:
		return fmt.Sprintf("Couldn't move the logins (auth.keyring): %s", err)
	case moved != 0 && keyring:
		return fmt.Sprintf("Moved %d logins and secrets to the OS keyring.", moved)
	case moved != 0:
		return fmt.Sprintf("Moved %d logins and secrets from the OS keyring back to mangal's database.", moved)
	}
	return ""
}

// authStore is the logins of the owner (e.g. "anilist"), in the OS keyring when auth.keyring.
func authStore(data *storage.DB, owner string) storage.KV {
	return storage.KV{DB: data, Table: storage.TableAuthTokens, Owner: owner, Keyring: keyring}
}

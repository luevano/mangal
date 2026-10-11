package cache

import (
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
	return storage.KV{DB: dataDB, Table: storage.TableAuthTokens, Owner: authOwnerMangaPlus}, err
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

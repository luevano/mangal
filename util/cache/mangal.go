package cache

import (
	"strings"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/storage"
)

const CacheDBNameMangal = storage.OwnerMangal

const (
	BucketNameSearchHistory = storage.BucketSearchHistory
)

const (
	MangasSearchHistory  = "mangas"
	AnilistSearchHistory = "anilist"
)

// get and set go through the store each time: mangal's stores are views over the
// databases, opened once per process, so there's nothing to keep open.
func get(dbName, bucketName, key string, v any) (bool, error) {
	store, err := CacheStore(dbName, bucketName)
	if err != nil {
		return false, err
	}
	return store.Get(key, v)
}

func set(dbName, bucketName, key string, v any) error {
	store, err := CacheStore(dbName, bucketName)
	if err != nil {
		return err
	}
	return store.Set(key, v)
}

// searchKeys are the search history's keys, each user's own (userKey).
var searchKeys = []string{MangasSearchHistory, AnilistSearchHistory, FillProviders}

// userKey is the local user's key of a search history ("david/mangas").
func userKey(key string) (string, error) {
	user, err := LocalUser()
	return user + "/" + key, err
}

func getMine(key string, v any) (bool, error) {
	k, err := userKey(key)
	if err != nil {
		return false, err
	}
	return get(CacheDBNameMangal, BucketNameSearchHistory, k, v)
}

func setMine(key string, v any) error {
	k, err := userKey(key)
	if err != nil {
		return err
	}
	return set(CacheDBNameMangal, BucketNameSearchHistory, k, v)
}

// LocalUser is the user mangal on this machine is without a login (auth.user, else the
// first admin, else storage.DefaultUser).
func LocalUser() (string, error) {
	if user := config.Auth.User.Get(); user != "" {
		return user, nil
	}
	st, err := Storage()
	if err != nil {
		return "", err
	}
	admin, err := st.Users().FirstAdmin()
	if err != nil || admin == "" {
		return storage.DefaultUser, err
	}
	return admin, nil
}

// ClaimUserData moves what's a user's (the search history, the logins: who's logged in,
// the logouts) from before 5.28 (nobody's) to the user's, or, with from set, from one user
// to another (a rename).
func ClaimUserData(from, user string) error {
	for _, set := range []struct {
		db, bucket string
		keys       []string
	}{
		{CacheDBNameMangal, BucketNameSearchHistory, searchKeys},
		{CacheDBNameMangalAuth, BucketNameAuthHistory, authKeys},
	} {
		store, err := CacheStore(set.db, set.bucket)
		if err != nil {
			return err
		}
		for _, key := range set.keys {
			old := key
			if from != "" {
				old = from + "/" + key
			}
			var v any
			switch {
			case key == FillProviders:
				v = new([]string)
			case set.bucket == BucketNameAuthHistory && strings.HasSuffix(key, "/logged_out"):
				v = new(bool)
			case set.bucket == BucketNameAuthHistory:
				v = new(UserHistory)
			default:
				v = new(Records)
			}
			found, err := store.Get(old, v)
			if err != nil {
				return err
			}
			if !found {
				continue
			}
			if err := store.Set(user+"/"+key, v); err != nil {
				return err
			}
			if err := store.Delete(old); err != nil {
				return err
			}
		}
	}
	return nil
}

// SetMangasSearchHistory will store the manga search history records to the cache.
func SetMangasSearchHistory(records Records) error {
	return setMine(MangasSearchHistory, records)
}

// GetMangasSearchHistory will populate the given records from the manga search history cache.
func GetMangasSearchHistory(records *Records) (bool, error) {
	return getMine(MangasSearchHistory, records)
}

// SetAnilistSearchHistory will store the manga search history records to the cache.
func SetAnilistSearchHistory(records Records) error {
	return setMine(AnilistSearchHistory, records)
}

// GetAnilistSearchHistory will populate the given records from the manga search history cache.
func GetAnilistSearchHistory(records *Records) (bool, error) {
	return getMine(AnilistSearchHistory, records)
}

// FillProviders is the key of the providers a fill searched last (the TUI's choice).
const FillProviders = "fill-providers"

// SetFillProviders stores the provider ids a fill searched.
func SetFillProviders(ids []string) error {
	return setMine(FillProviders, ids)
}

// GetFillProviders reads the provider ids a fill searched last, false when there are none.
func GetFillProviders(ids *[]string) (bool, error) {
	return getMine(FillProviders, ids)
}

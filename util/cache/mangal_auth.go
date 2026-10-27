package cache

import (
	"time"

	"github.com/luevano/libmangal/metadata/kitsu"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/storage/legacy"
	"golang.org/x/oauth2"
)

const CacheDBNameMangalAuth = storage.OwnerMangalAuth

const (
	BucketNameAuthHistory         = storage.BucketAuthHistory
	BucketNameAnilistAuthData     = legacy.BucketAnilistAuth
	BucketNameMyAnimeListAuthData = legacy.BucketMyAnimeListAuth
)

const (
	AnilistAuthHistory     = "anilist"
	MyAnimeListAuthHistory = "myanimelist"
)

// SetAuthHistory will store the local user's accounts of the provider (LocalUser: each
// mangal user has their own).
func SetAuthHistory(provider string, userHistory UserHistory) error {
	k, err := userKey(provider)
	if err != nil {
		return err
	}
	return set(CacheDBNameMangalAuth, BucketNameAuthHistory, k, userHistory)
}

// GetAuthHistory will populate the local user's accounts of the provider.
func GetAuthHistory(provider string, userHistory *UserHistory) (bool, error) {
	k, err := userKey(provider)
	if err != nil {
		return false, err
	}
	return get(CacheDBNameMangalAuth, BucketNameAuthHistory, k, userHistory)
}

// AuthAccount is the user's account of the provider (anilist, kitsu, myanimelist) their
// reads sync to: the last one they logged into, empty after a logout or without one.
func AuthAccount(user, provider string) (string, error) {
	var out bool
	if _, err := get(CacheDBNameMangalAuth, BucketNameAuthHistory, user+"/"+provider+"/logged_out", &out); err != nil || out {
		return "", err
	}
	var history UserHistory
	if _, err := get(CacheDBNameMangalAuth, BucketNameAuthHistory, user+"/"+provider, &history); err != nil {
		return "", err
	}
	return history.Last(), nil
}

// authKeys are the keys of each user's logins (their accounts, the logouts).
var authKeys = []string{
	AnilistAuthHistory, KitsuAuthHistory, MyAnimeListAuthHistory,
	anilistLoggedOut, kitsuLoggedOut, myAnimeListLoggedOut,
}

func setLoggedOut(key string, out bool) error {
	k, err := userKey(key)
	if err != nil {
		return err
	}
	return set(CacheDBNameMangalAuth, BucketNameAuthHistory, k, out)
}

func loggedOut(key string) (bool, error) {
	k, err := userKey(key)
	if err != nil {
		return false, err
	}
	var out bool
	_, err = get(CacheDBNameMangalAuth, BucketNameAuthHistory, k, &out)
	return out, err
}

// SetAnilistAuthData will store the oauth2 Token for the username.
func SetAnilistAuthData(username string, token *oauth2.Token) error {
	return set(CacheDBNameMangalAuth, BucketNameAnilistAuthData, username, *token)
}

// GetAnilistAuthData will populate the given oauth2 Token for the username.
func GetAnilistAuthData(username string, token *oauth2.Token) (bool, error) {
	return get(CacheDBNameMangalAuth, BucketNameAnilistAuthData, username, token)
}

// DeleteAnilistAuthData will delete the oauth2 Token assigned for the username.
//
// Doesn't return an error if not found.
func DeleteAnilistAuthData(username string) error {
	store, err := CacheStore(CacheDBNameMangalAuth, BucketNameAnilistAuthData)
	if err != nil {
		return err
	}
	return store.Delete(username)
}

// ExpireAnilistAuthData marks the username's oauth2 Token as expired (Anilist rejected it),
// so it isn't tried again and the user is told to log in again.
func ExpireAnilistAuthData(username string) error {
	var token oauth2.Token
	found, err := GetAnilistAuthData(username, &token)
	if err != nil || !found {
		return err
	}
	token.Expiry = time.Now()
	return SetAnilistAuthData(username, &token)
}

// anilistLoggedOut is the auth history key saying the user logged out of Anilist: the saved
// login isn't used on startup until the next login.
const anilistLoggedOut = AnilistAuthHistory + "/logged_out"

// SetAnilistLoggedOut saves whether the user logged out of Anilist.
func SetAnilistLoggedOut(out bool) error { return setLoggedOut(anilistLoggedOut, out) }

// AnilistLoggedOut is true if the user logged out of Anilist (and didn't log in since).
func AnilistLoggedOut() (bool, error) { return loggedOut(anilistLoggedOut) }

// KitsuAuthHistory is the auth history of Kitsu, and the owner of its logins.
const KitsuAuthHistory = "kitsu"

// SetKitsuAuthData stores the Kitsu token of the username (never the password).
func SetKitsuAuthData(username string, token kitsu.Token) error {
	return set(CacheDBNameMangalAuth, KitsuAuthHistory, username, token)
}

// GetKitsuAuthData populates the Kitsu token of the username.
func GetKitsuAuthData(username string, token *kitsu.Token) (bool, error) {
	return get(CacheDBNameMangalAuth, KitsuAuthHistory, username, token)
}

// DeleteKitsuAuthData deletes the Kitsu token of the username, no error if not found.
func DeleteKitsuAuthData(username string) error {
	store, err := CacheStore(CacheDBNameMangalAuth, KitsuAuthHistory)
	if err != nil {
		return err
	}
	return store.Delete(username)
}

// ExpireKitsuAuthData marks the username's Kitsu token as expired (Kitsu rejected it, or its
// refresh): it isn't tried again and can't be refreshed.
func ExpireKitsuAuthData(username string) error {
	var token kitsu.Token
	found, err := GetKitsuAuthData(username, &token)
	if err != nil || !found {
		return err
	}
	token.RefreshToken = ""
	token.ExpiresIn = max(token.ExpiresIn, 1)
	token.CreatedAt = time.Now().Unix() - int64(token.ExpiresIn)
	return SetKitsuAuthData(username, token)
}

const kitsuLoggedOut = KitsuAuthHistory + "/logged_out"

// SetKitsuLoggedOut saves whether the user logged out of Kitsu.
func SetKitsuLoggedOut(out bool) error { return setLoggedOut(kitsuLoggedOut, out) }

// KitsuLoggedOut is true if the user logged out of Kitsu (and didn't log in since).
func KitsuLoggedOut() (bool, error) { return loggedOut(kitsuLoggedOut) }

// SetMyAnimeListAuthData stores the oauth2 Token of the username (with its refresh token).
func SetMyAnimeListAuthData(username string, token *oauth2.Token) error {
	return set(CacheDBNameMangalAuth, BucketNameMyAnimeListAuthData, username, *token)
}

// GetMyAnimeListAuthData populates the oauth2 Token of the username.
func GetMyAnimeListAuthData(username string, token *oauth2.Token) (bool, error) {
	return get(CacheDBNameMangalAuth, BucketNameMyAnimeListAuthData, username, token)
}

// DeleteMyAnimeListAuthData deletes the oauth2 Token of the username, no error if not found.
func DeleteMyAnimeListAuthData(username string) error {
	store, err := CacheStore(CacheDBNameMangalAuth, BucketNameMyAnimeListAuthData)
	if err != nil {
		return err
	}
	return store.Delete(username)
}

// ExpireMyAnimeListAuthData marks the username's oauth2 Token as expired (MyAnimeList rejected
// it, or its refresh): it isn't tried again and can't be refreshed.
func ExpireMyAnimeListAuthData(username string) error {
	var token oauth2.Token
	found, err := GetMyAnimeListAuthData(username, &token)
	if err != nil || !found {
		return err
	}
	token.RefreshToken = ""
	if token.Expiry.IsZero() || token.Expiry.After(time.Now()) {
		token.Expiry = time.Now()
	}
	return SetMyAnimeListAuthData(username, &token)
}

const myAnimeListLoggedOut = MyAnimeListAuthHistory + "/logged_out"

// SetMyAnimeListLoggedOut saves whether the user logged out of MyAnimeList.
func SetMyAnimeListLoggedOut(out bool) error { return setLoggedOut(myAnimeListLoggedOut, out) }

// MyAnimeListLoggedOut is true if the user logged out of MyAnimeList (and didn't log in since).
func MyAnimeListLoggedOut() (bool, error) { return loggedOut(myAnimeListLoggedOut) }

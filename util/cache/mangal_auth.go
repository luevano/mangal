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

// SetAuthHistory will store the user history for the provider.
func SetAuthHistory(provider string, userHistory UserHistory) error {
	return set(CacheDBNameMangalAuth, BucketNameAuthHistory, provider, userHistory)
}

// GetAuthHistory will populate the given user history for the provider.
func GetAuthHistory(provider string, userHistory *UserHistory) (bool, error) {
	return get(CacheDBNameMangalAuth, BucketNameAuthHistory, provider, userHistory)
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
func SetAnilistLoggedOut(out bool) error {
	return set(CacheDBNameMangalAuth, BucketNameAuthHistory, anilistLoggedOut, out)
}

// AnilistLoggedOut is true if the user logged out of Anilist (and didn't log in since).
func AnilistLoggedOut() (bool, error) {
	var out bool
	_, err := get(CacheDBNameMangalAuth, BucketNameAuthHistory, anilistLoggedOut, &out)
	return out, err
}

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
func SetKitsuLoggedOut(out bool) error {
	return set(CacheDBNameMangalAuth, BucketNameAuthHistory, kitsuLoggedOut, out)
}

// KitsuLoggedOut is true if the user logged out of Kitsu (and didn't log in since).
func KitsuLoggedOut() (bool, error) {
	var out bool
	_, err := get(CacheDBNameMangalAuth, BucketNameAuthHistory, kitsuLoggedOut, &out)
	return out, err
}

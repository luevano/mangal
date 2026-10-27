package client

import (
	"time"

	"github.com/luevano/libmangal/metadata/kitsu"
	"github.com/luevano/mangal/v5/util/cache"
	"golang.org/x/oauth2"
)

// Tracker is a mangal user's account of a tracker (their reads sync there).
type Tracker struct {
	// Source is anilist, kitsu or myanimelist; Name its name.
	Source, Name string
	// Account is the user's account there, empty when logged out.
	Account string
	Expires time.Time
	// Login is what logging in asks for: code (AniList), address (MyAnimeList), password (Kitsu).
	Login string
	// Ready is false without the server's API client for it.
	Ready bool
}

// Expired is true once the account's login expired.
func (t Tracker) Expired() bool { return !t.Expires.IsZero() && time.Now().After(t.Expires) }

// Trackers are the user's accounts of AniList, Kitsu and MyAnimeList.
func Trackers(user string) ([]Tracker, error) {
	anilistID, _, err := cache.AnilistClient()
	if err != nil {
		return nil, err
	}
	malID, _, err := cache.MyAnimeListClient()
	if err != nil {
		return nil, err
	}
	list := []Tracker{
		{Source: cache.AnilistAuthHistory, Name: "AniList", Login: "code", Ready: anilistID != ""},
		{Source: cache.KitsuAuthHistory, Name: "Kitsu", Login: "password", Ready: true},
		{Source: cache.MyAnimeListAuthHistory, Name: "MyAnimeList", Login: "address", Ready: malID != ""},
	}
	for i := range list {
		t := &list[i]
		if t.Account, err = cache.AuthAccount(user, t.Source); err != nil || t.Account == "" {
			if err != nil {
				return nil, err
			}
			continue
		}
		switch t.Source {
		case cache.AnilistAuthHistory:
			var token oauth2.Token
			_, err = cache.GetAnilistAuthData(t.Account, &token)
			t.Expires = token.Expiry
		case cache.KitsuAuthHistory:
			var token kitsu.Token
			// a login that refreshes itself doesn't expire
			if _, err = cache.GetKitsuAuthData(t.Account, &token); token.AccessToken != "" && token.RefreshToken == "" {
				t.Expires = token.Expiry()
			}
		case cache.MyAnimeListAuthHistory:
			var token oauth2.Token
			if _, err = cache.GetMyAnimeListAuthData(t.Account, &token); token.RefreshToken == "" {
				t.Expires = token.Expiry
			}
		}
		if err != nil {
			return nil, err
		}
	}
	return list, nil
}

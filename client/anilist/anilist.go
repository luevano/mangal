package anilist

import (
	"log"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/libmangal/metadata/anilist"
	"github.com/luevano/mangal/util/cache"
	"golang.org/x/oauth2"
)

// TODO: handle the errors instead of just fatal'ing

var (
	anilist_    *metadata.ProviderWithCache
	anilistInit bool
)

func Anilist() *metadata.ProviderWithCache {
	if !anilistInit {
		anilist_ = newAnilist()
	}
	return anilist_
}

func newAnilist() *metadata.ProviderWithCache {
	aniOpts := anilist.DefaultOptions()

	ani, err := anilist.NewAnilist(aniOpts)
	if err != nil {
		log.Fatal(err)
	}

	opts := metadata.DefaultProviderWithCacheOptions()
	opts.Provider = ani
	opts.CacheStore = cache.CacheStore

	provider, err := metadata.NewProviderWithCache(opts)
	if err != nil {
		log.Fatal(err)
	}

	// TODO: handle cache error?
	//
	// Authenticate with the last authenticated user if existent
	var userHistory cache.UserHistory
	_, err = cache.GetAuthHistory(cache.AnilistAuthHistory, &userHistory)
	if err != nil {
		log.Fatal(err)
	}
	username := userHistory.Last()
	if username != "" {
		// TODO: handle cache error to also delete the username auth history?
		var token oauth2.Token
		found, _ := cache.GetAnilistAuthData(username, &token)
		if found {
			err = provider.Login(token.AccessToken)
		}
	}

	return provider
}

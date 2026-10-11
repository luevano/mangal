package cache

import (
	"encoding/gob"
	"fmt"
	"time"

	"github.com/luevano/libmangal/metadata/anilist"
	"github.com/luevano/libmangal/metadata/kitsu"
	"github.com/luevano/libmangal/metadata/myanimelist"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/storage"
	"github.com/philippgille/gokv"
	"golang.org/x/oauth2"
)

func init() {
	// CacheStore uses gob; types stored as interfaces must be registered first.
	// mangoprovider registers its own (mango-*, provider-metadata).

	// metadatas
	gob.RegisterName("anilist-manga", &anilist.Manga{})
	gob.RegisterName("myanimelist-manga", &myanimelist.Manga{})
	gob.RegisterName("kitsu-manga", &kitsu.Manga{})

	// metadata users
	gob.RegisterName("anilist-user", &anilist.User{})
	gob.RegisterName("myanimelist-user", &myanimelist.User{})
	gob.RegisterName("kitsu-user", &kitsu.User{})

	// oauth tokens
	gob.RegisterName("oauth-token", &oauth2.Token{})
	gob.RegisterName("kitsu-token", &kitsu.Token{})
}

// CacheStore is the store of a db name (a provider id, a metadata provider id, or mangal's
// own) and bucket name (the kind of data), see storage.Storage.Store.
func CacheStore(dbName, bucketName string) (gokv.Store, error) {
	st, err := Storage()
	if err != nil {
		return nil, err
	}
	opts, err := storeOptions()
	if err != nil {
		return nil, err
	}
	return st.Store(dbName, bucketName, opts), nil
}

func storeOptions() (storage.StoreOptions, error) {
	ttl, err := time.ParseDuration(config.Cache.TTL.Get())
	if err != nil {
		return storage.StoreOptions{}, fmt.Errorf("cache.ttl: %w", err)
	}
	mirrorTTL, err := time.ParseDuration(config.Cache.MirrorTTL.Get())
	if err != nil {
		return storage.StoreOptions{}, fmt.Errorf("cache.mirror_ttl: %w", err)
	}
	return storage.StoreOptions{TTL: ttl, MirrorTTL: mirrorTTL, Keyring: keyring}, nil
}

package cache

import (
	"encoding/gob"
	"time"

	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/libmangal/metadata/anilist"
	"github.com/luevano/libmangal/metadata/myanimelist"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/storage"
	mango "github.com/luevano/mangoprovider"
	"github.com/philippgille/gokv"
	"golang.org/x/oauth2"
)

func init() {
	// CacheStore uses gob; types that will be stores must be registered first

	// manga data
	gob.RegisterName("mango-manga", &mango.Manga{})
	gob.RegisterName("mango-volume", &mango.Volume{})
	gob.RegisterName("mango-chapter", &mango.Chapter{})
	gob.RegisterName("mango-page", &mango.Page{})

	// metadatas
	gob.RegisterName("provider-metadata", &mangadata.Metadata{})
	gob.RegisterName("anilist-manga", &anilist.Manga{})
	gob.RegisterName("myanimelist-manga", &myanimelist.Manga{})

	// metadata users
	gob.RegisterName("anilist-user", &anilist.User{})
	gob.RegisterName("myanimelist-user", &myanimelist.User{})

	// oauth tokens
	gob.RegisterName("oauth-token", &oauth2.Token{})
}

// CacheStore is the general store builder with a provided db name (a provider id, a
// metadata provider id, or mangal's own) and bucket name (the kind of data).
//
// Each kind of data has its own table, in the cache database or, for what isn't cache
// (title bindings, logins, search history), in the data database. The TTL is decided by
// the kind.
func CacheStore(dbName, bucketName string) (gokv.Store, error) {
	cacheDB, dataDB, err := open()
	if err != nil {
		return nil, err
	}
	ttl, err := time.ParseDuration(config.Cache.TTL.Get())
	if err != nil {
		return nil, err
	}
	metadataTTL := 48 * time.Hour

	switch {
	case dbName == CacheDBNameMangalAuth && bucketName == BucketNameAuthHistory:
		return storage.KV{DB: dataDB, Table: storage.TableAuthHistory}, nil
	case dbName == CacheDBNameMangalAuth:
		// no expiry, an expired token is kept (marked, see ExpireAnilistAuthData) to tell the user
		return authStore(dataDB, bucketName), nil
	case dbName == CacheDBNameMangal && bucketName == BucketNameSearchHistory:
		return storage.KV{DB: dataDB, Table: storage.TableSearchHistory}, nil
	case bucketName == metadata.CacheBucketNameTitleToID:
		return storage.KV{DB: dataDB, Table: storage.TableTitleBindings, Owner: dbName}, nil
	case bucketName == metadata.CacheBucketNameQueryToIDs:
		return storage.KV{DB: cacheDB, Table: storage.TableMetadataSearches, Owner: dbName, TTL: metadataTTL}, nil
	case bucketName == metadata.CacheBucketNameIDToManga:
		return storage.KV{DB: cacheDB, Table: storage.TableMetadata, Owner: dbName, TTL: metadataTTL}, nil
	case bucketName == mango.CacheBucketNameMangas:
		return storage.KV{DB: cacheDB, Table: storage.TableMangaSearches, Owner: dbName, TTL: ttl}, nil
	case bucketName == mango.CacheBucketNameVolumes:
		return storage.KV{DB: cacheDB, Table: storage.TableVolumes, Owner: dbName, TTL: ttl}, nil
	case bucketName == mango.CacheBucketNameChapters:
		return storage.KV{DB: cacheDB, Table: storage.TableChapters, Owner: dbName, TTL: ttl}, nil
	case bucketName == dbName:
		// luaprovider caches http responses with the provider id as the bucket
		return storage.KV{DB: cacheDB, Table: storage.TableLuaHTTP, Owner: dbName, TTL: ttl}, nil
	default:
		return storage.KV{DB: cacheDB, Table: storage.TableOther, Owner: dbName + "/" + bucketName, TTL: ttl}, nil
	}
}

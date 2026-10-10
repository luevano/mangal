package cache

import (
	"encoding/gob"
	"errors"
	"path/filepath"
	"slices"
	"time"

	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/libmangal/metadata/anilist"
	"github.com/luevano/libmangal/metadata/myanimelist"
	"github.com/luevano/mangal/config"
	"github.com/luevano/mangal/path"
	"github.com/luevano/mangal/util/afs"
	"github.com/luevano/mangal/util/cache/bbolt"
	mango "github.com/luevano/mangoprovider"
	"github.com/philippgille/gokv"
	"github.com/philippgille/gokv/encoding"
	"golang.org/x/oauth2"
)

const metadataDir = "metadata"

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

// TODO: add subdir as parameter to avoid checking if the db is supposed to go into metadata dir
//
// CacheStore is the general cache store builder with a provided db name and bucket name.
//
// The TTL is decided based on the db name or bucket name.
func CacheStore(dbName, bucketName string) (gokv.Store, error) {
	dir, err := cacheDir(path.CacheDir(), dbName)
	if err != nil {
		return nil, err
	}

	// TTL
	ttl, err := time.ParseDuration(config.Cache.TTL.Get())
	if err != nil {
		return nil, err
	}
	switch bucketName {
	case bbolt.TTLBucketName:
		return nil, errors.New(`can't use reserved bucket name "` + bbolt.TTLBucketName + `"`)
	case metadata.CacheBucketNameQueryToIDs:
		ttl = time.Hour * 24 * 2
	case metadata.CacheBucketNameTitleToID:
		// TODO: keeping the same old behavior, need to rework this;
		// the infinite TTL is implemented now
		ttl = time.Hour * 9999
	case metadata.CacheBucketNameIDToManga:
		ttl = time.Hour * 24 * 2
	case BucketNameAuthHistory,
		BucketNameAnilistAuthData,
		BucketNameMyAnimeListAuthData:
		// no expiry on the auth data, when the
		// tokens expire, delete the individual
		// auth data and prompt for re-authenticat
		ttl = 0
	case BucketNameSearchHistory:
		ttl = 0 // no expiry
	}

	return bbolt.NewStore(bbolt.Options{
		TTL:        ttl,
		BucketName: bucketName,
		Path:       filepath.Join(dir, dbName+".db"),
		Codec:      encoding.Gob,
	})
}

// metadataDBNames are the metadata providers' ids (libmangal's metadata.ProviderInfo.ID,
// their cache db name).
var metadataDBNames = []string{"anilist", "myanimelist", "kitsu", "mangaupdates", "animeplanet"}

// cacheDir is the directory of the db: metadata cache dbs go into the subdir "metadata".
//
// They used to land in the cache root, a db there is moved over once.
func cacheDir(root, dbName string) (string, error) {
	dir := root
	if slices.Contains(metadataDBNames, dbName) {
		dir = filepath.Join(root, metadataDir)
	}
	if err := afs.Afero.MkdirAll(dir, config.Download.ModeDir.Get()); err != nil {
		return "", err
	}
	if dir == root {
		return dir, nil
	}

	old, moved := filepath.Join(root, dbName+".db"), filepath.Join(dir, dbName+".db")
	oldExists, err := afs.Afero.Exists(old)
	if err != nil || !oldExists {
		return dir, err
	}
	if exists, err := afs.Afero.Exists(moved); err != nil || exists {
		return dir, err
	}
	return dir, afs.Afero.Rename(old, moved)
}

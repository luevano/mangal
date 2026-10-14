// Package legacy converts the old bbolt cache files (mangal 5.12 and older) into the data
// database.
//
// TODO: retire it (delete this package, its call in util/cache and run go mod tidy to drop
// bbolt) once most users have converted; the release or date is still to be picked.
package legacy

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/storage"
	mango "github.com/luevano/mangoprovider"
	bolt "go.etcd.io/bbolt"
	bolterrors "go.etcd.io/bbolt/errors"
)

// The old auth buckets of mangal-auth.db, the owners of their logins now.
const (
	BucketAnilistAuth     = "anilist"
	BucketMyAnimeListAuth = "myanimelist"
)

// metadataDBs are the metadata providers' old cache files, they hold the title bindings.
var metadataDBs = []string{"anilist", "myanimelist", "kitsu", "mangaupdates", "animeplanet"}

// knownBuckets are every bucket the old cache files had, besides a bucket named after the
// file (luaprovider's http cache, mangal 5.10's anilist stores).
var knownBuckets = append(slices.Clone(mango.LegacyCacheBucketNames),
	metadata.CacheBucketNameTitleToID, metadata.CacheBucketNameQueryToIDs, metadata.CacheBucketNameIDToManga,
	storage.BucketSearchHistory, storage.BucketAuthHistory, BucketAnilistAuth, BucketMyAnimeListAuth, "access-token",
)

// counts are what a conversion kept.
type counts struct {
	bindings, logins, histories int
	oldLogin                    bool
}

// Import converts the old bbolt cache files ("<id>.db" in the cache directory and its
// "metadata" subdirectory; mangal 5.10 and older, "anilist/<store>.db"): what isn't cache
// (title bindings, logins, search history) goes into the data database, the rest rebuilds.
// Only files with a known name or layout are converted and then deleted, anything else is
// left alone.
//
// It never fails: the notice (empty if there was nothing to convert) tells what happened,
// including the files it couldn't convert.
func Import(cacheDir string, data *storage.DB) (notice string) {
	files, _ := filepath.Glob(filepath.Join(cacheDir, "*.db"))
	metadataFiles, _ := filepath.Glob(filepath.Join(cacheDir, "metadata", "*.db"))
	anilistFiles, _ := filepath.Glob(filepath.Join(cacheDir, "anilist", "*.db"))
	files = append(files, metadataFiles...)
	files = append(files, anilistFiles...)

	var total counts
	var converted int
	var inUse, failed []string
	for _, file := range files {
		c, ok, err := importFile(file, data)
		switch {
		case errors.Is(err, bolterrors.ErrTimeout):
			inUse = append(inUse, filepath.Base(file))
		case err != nil:
			failed = append(failed, fmt.Sprintf("%s (%s)", file, err))
		case ok:
			converted++
			total.bindings += c.bindings
			total.logins += c.logins
			total.histories += c.histories
			total.oldLogin = total.oldLogin || c.oldLogin
		}
	}
	// only removed if empty
	_ = os.Remove(filepath.Join(cacheDir, "metadata"))
	_ = os.Remove(filepath.Join(cacheDir, "anilist"))

	var parts []string
	if converted != 0 {
		parts = append(parts, fmt.Sprintf("Converted the old cache to the new storage: kept %d title bindings, %d logins and %d histories, the rest of the cache rebuilds as you use mangal.", total.bindings, total.logins, total.histories))
	}
	if total.oldLogin {
		parts = append(parts, "The AniList login from mangal 5.10 or older can't be kept, log in again.")
	}
	if len(inUse) != 0 {
		parts = append(parts, fmt.Sprintf("Old cache files %s are in use by another mangal, close it to convert them too.", strings.Join(inUse, ", ")))
	}
	if len(failed) != 0 {
		parts = append(parts, fmt.Sprintf("Couldn't convert the old cache files %s, they're kept; delete them to stop this message.", strings.Join(failed, ", ")))
	}
	return strings.Join(parts, " ")
}

// importFile converts one file and deletes it, ok is false if it isn't one of the old
// cache files (and is kept).
func importFile(file string, data *storage.DB) (c counts, ok bool, err error) {
	db, err := bolt.Open(file, 0o600, &bolt.Options{Timeout: time.Second, ReadOnly: true})
	if errors.Is(err, bolterrors.ErrTimeout) {
		return c, false, err
	}
	if err != nil {
		// not a bbolt file, not one of ours
		return c, false, nil
	}

	name := strings.TrimSuffix(filepath.Base(file), ".db")
	old := filepath.Base(filepath.Dir(file)) == "anilist"
	err = db.View(func(btx *bolt.Tx) error {
		if (old || !knownName(name)) && !knownLayout(btx, name) {
			return nil
		}
		ok = true
		return data.InTx(func(tx *sql.Tx) error {
			c = counts{}
			var err error
			n := 0
			switch {
			case old && name == metadata.CacheBucketNameTitleToID:
				n, err = copyBucket(btx, name, tx, storage.TableTitleBindings, "anilist")
				c.bindings += n
			case old && name == "access-token":
				// a bare token without the user it belongs to, it needs a new login
				if b := btx.Bucket([]byte(name)); b != nil && b.Stats().KeyN != 0 {
					c.oldLogin = true
				}
			case old:
				// the rest of the old anilist stores is cache
			case name == storage.OwnerMangal:
				n, err = copyBucket(btx, storage.BucketSearchHistory, tx, storage.TableSearchHistory, "")
				c.histories += n
			case name == storage.OwnerMangalAuth:
				n, err = copyBucket(btx, storage.BucketAuthHistory, tx, storage.TableAuthHistory, "")
				c.histories += n
				for _, bucket := range []string{BucketAnilistAuth, BucketMyAnimeListAuth} {
					if err == nil {
						n, err = copyBucket(btx, bucket, tx, storage.TableAuthTokens, bucket)
						c.logins += n
					}
				}
			case slices.Contains(metadataDBs, name):
				n, err = copyBucket(btx, metadata.CacheBucketNameTitleToID, tx, storage.TableTitleBindings, name)
				c.bindings += n
			}
			return err
		})
	})
	_ = db.Close()
	if err != nil || !ok {
		return c, false, err
	}
	if err := os.Remove(file); err != nil {
		return c, false, err
	}
	return c, true, nil
}

func knownName(name string) bool {
	return name == storage.OwnerMangal || name == storage.OwnerMangalAuth || slices.Contains(metadataDBs, name)
}

// knownLayout is true for a file whose buckets are all ones the old cache had (a provider's).
func knownLayout(tx *bolt.Tx, name string) bool {
	buckets := 0
	known := true
	_ = tx.ForEach(func(bucket []byte, _ *bolt.Bucket) error {
		buckets++
		if b := string(bucket); b != name && !slices.Contains(knownBuckets, b) {
			known = false
		}
		return nil
	})
	return buckets != 0 && known
}

// copyBucket copies the raw (gob) values of a bucket into a kv table, keeping what the
// table already has.
func copyBucket(btx *bolt.Tx, bucket string, tx *sql.Tx, table, owner string) (int, error) {
	b := btx.Bucket([]byte(bucket))
	if b == nil {
		return 0, nil
	}
	n := 0
	err := b.ForEach(func(k, v []byte) error {
		res, err := tx.Exec(`INSERT OR IGNORE INTO `+table+` (owner, key, value, expires) VALUES (?, ?, ?, 0)`, owner, string(k), v)
		if err != nil {
			return err
		}
		if added, _ := res.RowsAffected(); added > 0 {
			n++
		}
		return nil
	})
	return n, err
}

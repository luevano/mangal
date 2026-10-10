package cache

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/storage"
	bolt "go.etcd.io/bbolt"
)

// legacyMetadataDBs are the metadata providers' old cache files, they hold the title bindings.
var legacyMetadataDBs = []string{"anilist", "myanimelist", "kitsu", "mangaupdates", "animeplanet"}

// importBBolt converts the old bbolt cache files (mangal 5.12 and older, "<id>.db" in the
// cache directory and its "metadata" subdirectory): what isn't cache (title bindings,
// logins, search history) goes into the data database, the rest rebuilds. The old files
// are deleted.
//
// TODO: keep for a while, until most users have converted.
func importBBolt(cacheDir string, data *storage.DB) (string, error) {
	files, _ := filepath.Glob(filepath.Join(cacheDir, "*.db"))
	metadataFiles, _ := filepath.Glob(filepath.Join(cacheDir, "metadata", "*.db"))
	files = append(files, metadataFiles...)
	if len(files) == 0 {
		return "", nil
	}

	var bindings, logins, histories int
	var inUse []string
	for _, file := range files {
		db, err := bolt.Open(file, 0o600, &bolt.Options{Timeout: time.Second, ReadOnly: true})
		if errors.Is(err, bolt.ErrTimeout) {
			inUse = append(inUse, filepath.Base(file))
			continue
		}
		if err != nil {
			// not one of ours
			continue
		}

		name := strings.TrimSuffix(filepath.Base(file), ".db")
		err = db.View(func(tx *bolt.Tx) error {
			var err error
			n := 0
			switch {
			case name == CacheDBNameMangal:
				n, err = copyBucket(tx, BucketNameSearchHistory, data, storage.TableSearchHistory, "")
				histories += n
			case name == CacheDBNameMangalAuth:
				n, err = copyBucket(tx, BucketNameAuthHistory, data, storage.TableAuthHistory, "")
				histories += n
				for _, bucket := range []string{BucketNameAnilistAuthData, BucketNameMyAnimeListAuthData} {
					if err == nil {
						n, err = copyBucket(tx, bucket, data, storage.TableAuthTokens, bucket)
						logins += n
					}
				}
			case slices.Contains(legacyMetadataDBs, name):
				n, err = copyBucket(tx, metadata.CacheBucketNameTitleToID, data, storage.TableTitleBindings, name)
				bindings += n
			}
			return err
		})
		_ = db.Close()
		if err != nil {
			return "", fmt.Errorf("convert the old cache %s: %w", file, err)
		}
		if err := os.Remove(file); err != nil {
			return "", fmt.Errorf("convert the old cache: %w", err)
		}
	}
	// only removed if empty
	_ = os.Remove(filepath.Join(cacheDir, "metadata"))

	notice := fmt.Sprintf("Converted the old cache to the new storage: kept %d title bindings, %d logins and %d histories, the rest of the cache rebuilds as you use mangal.", bindings, logins, histories)
	if len(inUse) != 0 {
		notice += fmt.Sprintf(" %s are in use by another mangal, close it to convert them too.", strings.Join(inUse, ", "))
	}
	return notice, nil
}

// copyBucket copies the raw (gob) values of a bucket into a kv table, keeping what the
// table already has.
func copyBucket(tx *bolt.Tx, bucket string, data *storage.DB, table, owner string) (int, error) {
	b := tx.Bucket([]byte(bucket))
	if b == nil {
		return 0, nil
	}
	n := 0
	err := b.ForEach(func(k, v []byte) error {
		res, err := data.Exec(`INSERT OR IGNORE INTO `+table+` (owner, key, value, expires) VALUES (?, ?, ?, 0)`, owner, string(k), v)
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

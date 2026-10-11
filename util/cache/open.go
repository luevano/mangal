package cache

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/luevano/libmangal"

	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/storage/legacy"
)

var (
	openOnce sync.Once
	st       storage.Storage
	openErr  error
	notices  []string
)

// Open opens the cache and data databases (once per process) and converts the old
// cache files, if any.
func Open() error {
	_, err := Storage()
	return err
}

// Close closes the databases, if they were opened. Only when mangal exits.
func Close() error {
	var errs []error
	for _, db := range []*storage.DB{st.Cache, st.Data} {
		if db != nil {
			errs = append(errs, db.Close())
		}
	}
	return errors.Join(errs...)
}

// Notices are messages for the user from opening the storage (e.g. the old cache was
// converted), to show once.
func Notices() []string {
	return notices
}

// Storage is mangal's databases, opened once per process.
func Storage() (storage.Storage, error) {
	openOnce.Do(func() {
		var cacheDB, dataDB *storage.DB
		cacheDB, openErr = storage.OpenCache(filepath.Join(path.CacheDir(), storage.FilenameCache))
		if openErr != nil {
			return
		}
		dataDB, openErr = storage.OpenData(filepath.Join(path.DataDir(), storage.FilenameData))
		if openErr != nil {
			_ = cacheDB.Close()
			return
		}
		st = storage.Storage{Cache: cacheDB, Data: dataDB}
		if notice := legacy.Import(path.CacheDir(), dataDB); notice != "" {
			notices = append(notices, notice)
		}
		if notice := useKeyring(dataDB); notice != "" {
			notices = append(notices, notice)
		}
	})
	return st, openErr
}

// Library is the library index.
func Library() (storage.Library, error) {
	s, err := Storage()
	return s.Library(), err
}

// AccountsStore is the cached provider accounts, by provider id.
func AccountsStore(ttl time.Duration) (storage.KV, error) {
	s, err := Storage()
	return s.AccountsStore(ttl), err
}

// DBs returns the cache and data databases.
//
// Deprecated: use Storage, Library or AccountsStore.
func DBs() (cache, data *storage.DB, err error) {
	s, err := Storage()
	return s.Cache, s.Data, err
}

// OldIDsNotice tells about data still under old provider ids, short for a one line
// warning and long to explain what migrating does. Both are empty if there's none.
func OldIDsNotice(loaders []libmangal.ProviderLoader) (short, long string, err error) {
	s, err := Storage()
	if err != nil {
		return "", "", err
	}
	oldIDs, err := s.FindOldIDs(loaders)
	if err != nil || len(oldIDs) == 0 {
		return "", "", err
	}

	var renames, details []string
	for _, id := range oldIDs {
		renames = append(renames, fmt.Sprintf("%s is now %s", id.Old, id.New))
		details = append(details, fmt.Sprintf("- %s is now %s (%s): %d library mangas, %d downloads, %d cache entries", id.Old, id.New, id.Name, id.Sources, id.Downloads, id.Cache))
	}
	short = fmt.Sprintf("data under old provider ids (%s), run %q to update it", strings.Join(renames, ", "), "mangal migrate")
	long = "Some data still uses old provider ids:\n\n" + strings.Join(details, "\n") + `

Press M on the home screen (or run "mangal migrate") to see what would change and do it:
the library entries, downloads and .libmangal.json files move to the new ids and the old
cache is dropped (it rebuilds). Folders aren't renamed or moved, "mangal library rename"
does that. Until then everything keeps working through the old ids.`
	return short, long, nil
}

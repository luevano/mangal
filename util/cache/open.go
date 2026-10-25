package cache

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/luevano/libmangal"

	"github.com/luevano/mangal/v5/config"
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

// AddNotice adds a message to Notices, from what runs right after opening the storage
// (the settings import).
func AddNotice(notice string) {
	if !slices.Contains(notices, notice) {
		notices = append(notices, notice)
	}
}

// Storage is mangal's databases, opened once per process.
func Storage() (storage.Storage, error) {
	openOnce.Do(func() {
		var cacheDB, dataDB *storage.DB
		cacheDB, openErr = storage.OpenCache(filepath.Join(path.Cache(), storage.FilenameCache))
		if openErr != nil {
			return
		}
		dataDB, openErr = storage.OpenData(filepath.Join(path.Data(), storage.FilenameData))
		if openErr != nil {
			_ = cacheDB.Close()
			return
		}
		st = storage.Storage{Cache: cacheDB, Data: dataDB}
		if notice := legacy.Import(path.Cache(), dataDB); notice != "" {
			notices = append(notices, notice)
		}
		if notice := useKeyring(dataDB); notice != "" {
			notices = append(notices, notice)
		}
		if notice := moveConfigClient(dataDB); notice != "" {
			notices = append(notices, notice)
		}
		if notice := moveServeTokens(dataDB); notice != "" {
			notices = append(notices, notice)
		}
		if err := config.UseStore(st.Settings(keyring)); err != nil {
			notices = append(notices, fmt.Sprintf("Couldn't read the settings from the database, the defaults are used: %s", err))
		}
	})
	return st, openErr
}

// Settings are the settings stored in the data database (secrets with the logins).
func Settings() (storage.Settings, error) {
	s, err := Storage()
	return s.Settings(keyring), err
}

// Library is the library index.
func Library() (storage.Library, error) {
	s, err := Storage()
	return s.Library(), err
}

// History is the read history.
func History() (storage.History, error) {
	s, err := Storage()
	return s.History(), err
}

// Watches are the watched mangas and their checks.
func Watches() (storage.Watches, error) {
	s, err := Storage()
	return s.Watches(), err
}

// Bindings are the metadata bindings of the provider mangas.
func Bindings() (storage.Bindings, error) {
	s, err := Storage()
	return s.Bindings(), err
}

// ClearChapterCache forgets the provider's cached chapter lists, see
// storage.Storage.ClearChapterCache.
func ClearChapterCache(provider string) error {
	s, err := Storage()
	if err != nil {
		return err
	}
	_, err = s.ClearChapterCache(provider)
	return err
}

// AccountsStore is the cached provider accounts, by provider id.
func AccountsStore(ttl time.Duration) (storage.KV, error) {
	s, err := Storage()
	return s.AccountsStore(ttl), err
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
		details = append(details, fmt.Sprintf("- %s is now %s (%s): %d library mangas, %d downloads, %d read chapters, %d watches, %d metadata bindings, %d cache entries", id.Old, id.New, id.Name, id.Sources, id.Downloads, id.Reads, id.Watches, id.Bindings, id.Cache))
	}
	short = fmt.Sprintf("data under old provider ids (%s), run %q to update it", strings.Join(renames, ", "), "mangal migrate")
	long = "Some data still uses old provider ids:\n\n" + strings.Join(details, "\n") + `

Press X on the home screen (or run "mangal migrate") to see what would change and do it:
the library entries, downloads, read history, watches and .libmangal.json files move to the new
ids and the old cache is dropped (it rebuilds). Folders aren't renamed or moved, "mangal
library rename" does that. Until then everything keeps working through the old ids.`
	return short, long, nil
}

// AuthFingerprint is Storage().AuthFingerprint().
func AuthFingerprint() (string, error) {
	st, err := Storage()
	if err != nil {
		return "", err
	}
	return st.AuthFingerprint()
}

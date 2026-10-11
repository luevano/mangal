package cache

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/luevano/libmangal"

	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/storage"
)

var (
	openOnce        sync.Once
	cacheDB, dataDB *storage.DB
	openErr         error
	notices         []string
)

// Open opens the cache and data databases (once per process) and converts the old
// cache files, if any.
func Open() error {
	_, _, err := open()
	return err
}

// Notices are messages for the user from opening the storage (e.g. the old cache was
// converted), to show once.
func Notices() []string {
	return notices
}

// DBs returns the cache and data databases.
func DBs() (cache, data *storage.DB, err error) {
	return open()
}

func open() (*storage.DB, *storage.DB, error) {
	openOnce.Do(func() {
		cacheDB, openErr = storage.OpenCache(filepath.Join(path.CacheDir(), storage.FilenameCache))
		if openErr != nil {
			return
		}
		dataDB, openErr = storage.OpenData(filepath.Join(path.DataDir(), storage.FilenameData))
		if openErr != nil {
			return
		}
		var notice string
		notice, openErr = importBBolt(path.CacheDir(), dataDB)
		if notice != "" {
			notices = append(notices, notice)
		}
		if notice := useKeyring(dataDB); notice != "" {
			notices = append(notices, notice)
		}
	})
	return cacheDB, dataDB, openErr
}

// OldIDsNotice tells about data still under old provider ids, short for a one line
// warning and long to explain what migrating does. Both are empty if there's none.
func OldIDsNotice(loaders []libmangal.ProviderLoader) (short, long string, err error) {
	cacheDB, dataDB, err := open()
	if err != nil {
		return "", "", err
	}
	oldIDs, err := storage.FindOldIDs(cacheDB, dataDB, loaders)
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

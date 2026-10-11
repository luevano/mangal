package storage

import (
	"database/sql"
	"fmt"
	"slices"

	"github.com/luevano/libmangal"
)

// OldID is a provider id still in the database that is now an old id of a provider
// (in its libmangal.ProviderInfo.History).
type OldID struct {
	Old, New, Name string
	// Sources is how many library mangas were downloaded from the old id.
	Sources int
	// Downloads is how many downloaded chapters use the old id.
	Downloads int
	// Cache is how many cache entries use the old id (dropped when migrating).
	Cache int
}

// FindOldIDs lists the provider ids in the databases that are old ids of the loaders.
func (s Storage) FindOldIDs(loaders []libmangal.ProviderLoader) ([]OldID, error) {
	return FindOldIDs(s.Cache, s.Data, loaders)
}

// MigrateOldIDs moves the data of the old ids to their new ids, see MigrateOldIDs.
func (s Storage) MigrateOldIDs(oldIDs []OldID) error {
	return MigrateOldIDs(s.Cache, s.Data, oldIDs)
}

// FindOldIDs lists the provider ids in the databases that are old ids of the loaders.
func FindOldIDs(cache, data *DB, loaders []libmangal.ProviderLoader) ([]OldID, error) {
	var candidates []string
	for _, loader := range loaders {
		for _, alias := range loader.Info().History {
			// a current id (also an alias that kept it) isn't old
			if _, a, ok := libmangal.FindLoader(loaders, alias.ID); ok && a != nil && !slices.Contains(candidates, alias.ID) {
				candidates = append(candidates, alias.ID)
			}
		}
	}
	slices.Sort(candidates)

	var oldIDs []OldID
	for _, id := range candidates {
		loader, _, _ := libmangal.FindLoader(loaders, id)
		old := OldID{Old: id, New: loader.Info().ID, Name: loader.Info().Name}
		if err := data.QueryRow(`SELECT count(*) FROM library_sources WHERE provider = ?`, id).Scan(&old.Sources); err != nil {
			return nil, fmt.Errorf("count the library sources of %s: %w", id, err)
		}
		if err := data.QueryRow(`SELECT count(*) FROM downloads WHERE provider = ?`, id).Scan(&old.Downloads); err != nil {
			return nil, fmt.Errorf("count the downloads of %s: %w", id, err)
		}
		n, err := countProviderRows(cache, id)
		if err != nil {
			return nil, fmt.Errorf("count the cache of %s: %w", id, err)
		}
		old.Cache = n
		if old.Sources+old.Downloads+old.Cache != 0 {
			oldIDs = append(oldIDs, old)
		}
	}
	return oldIDs, nil
}

// MigrateOldIDs moves the library sources and downloads of each old id to its new id and drops
// its cache entries (they're rebuilt under the new id).
func MigrateOldIDs(cache, data *DB, oldIDs []OldID) error {
	err := data.InTx(func(tx *sql.Tx) error {
		for _, id := range oldIDs {
			// a source already under the new id wins
			if _, err := tx.Exec(`DELETE FROM library_sources WHERE provider = ? AND manga_id IN (SELECT manga_id FROM library_sources WHERE provider = ?)`, id.Old, id.New); err != nil {
				return fmt.Errorf("migrate %s: %w", id.Old, err)
			}
			if _, err := tx.Exec(`UPDATE library_sources SET provider = ? WHERE provider = ?`, id.New, id.Old); err != nil {
				return fmt.Errorf("migrate %s: %w", id.Old, err)
			}
			if _, err := tx.Exec(`UPDATE downloads SET provider = ? WHERE provider = ?`, id.New, id.Old); err != nil {
				return fmt.Errorf("migrate %s: %w", id.Old, err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return cache.InTx(func(tx *sql.Tx) error {
		for _, id := range oldIDs {
			for _, table := range CacheTables {
				if _, err := tx.Exec(`DELETE FROM `+table+` WHERE `+providerRows, id.Old); err != nil {
					return fmt.Errorf("drop the cache of %s: %w", id.Old, err)
				}
			}
		}
		return nil
	})
}

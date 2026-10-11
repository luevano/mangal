package storage

import (
	"fmt"
	"slices"

	"github.com/luevano/libmangal"
)

// providerTables are the cache tables owned by manga providers.
var providerTables = []string{TableMangaSearches, TableVolumes, TableChapters, TableLuaHTTP}

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
func FindOldIDs(cache, data *DB, loaders []libmangal.ProviderLoader) ([]OldID, error) {
	counts := map[string]*OldID{}
	var order []string
	add := func(id string, dirs, downloads, entries int) {
		if _, alias, ok := libmangal.FindLoader(loaders, id); !ok || alias == nil {
			return
		}
		if _, ok := counts[id]; !ok {
			loader, _, _ := libmangal.FindLoader(loaders, id)
			counts[id] = &OldID{Old: id, New: loader.Info().ID, Name: loader.Info().Name}
			order = append(order, id)
		}
		counts[id].Sources += dirs
		counts[id].Downloads += downloads
		counts[id].Cache += entries
	}

	if err := countBy(data, `SELECT provider, count(*) FROM library_sources GROUP BY provider`, func(id string, n int) { add(id, n, 0, 0) }); err != nil {
		return nil, err
	}
	if err := countBy(data, `SELECT provider, count(*) FROM downloads GROUP BY provider`, func(id string, n int) { add(id, 0, n, 0) }); err != nil {
		return nil, err
	}
	for _, table := range providerTables {
		if err := countBy(cache, `SELECT owner, count(*) FROM `+table+` GROUP BY owner`, func(id string, n int) { add(id, 0, 0, n) }); err != nil {
			return nil, err
		}
	}

	slices.Sort(order)
	oldIDs := make([]OldID, len(order))
	for i, id := range order {
		oldIDs[i] = *counts[id]
	}
	return oldIDs, nil
}

func countBy(db *DB, query string, f func(string, int)) error {
	rows, err := db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return err
		}
		f(id, n)
	}
	return rows.Err()
}

// MigrateOldIDs moves the library sources and downloads of each old id to its new id and drops
// its cache entries (they're rebuilt under the new id).
func MigrateOldIDs(cache, data *DB, oldIDs []OldID) error {
	for _, id := range oldIDs {
		// a source already under the new id wins
		if _, err := data.Exec(`DELETE FROM library_sources WHERE provider = ? AND manga_id IN (SELECT manga_id FROM library_sources WHERE provider = ?)`, id.Old, id.New); err != nil {
			return fmt.Errorf("migrate %s: %w", id.Old, err)
		}
		if _, err := data.Exec(`UPDATE library_sources SET provider = ? WHERE provider = ?`, id.New, id.Old); err != nil {
			return fmt.Errorf("migrate %s: %w", id.Old, err)
		}
		if _, err := data.Exec(`UPDATE downloads SET provider = ? WHERE provider = ?`, id.New, id.Old); err != nil {
			return fmt.Errorf("migrate %s: %w", id.Old, err)
		}
		for _, table := range providerTables {
			if _, err := cache.Exec(`DELETE FROM `+table+` WHERE owner = ?`, id.Old); err != nil {
				return fmt.Errorf("migrate %s: %w", id.Old, err)
			}
		}
	}
	return nil
}

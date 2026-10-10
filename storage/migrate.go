package storage

import (
	"encoding/json"
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
	// MangaDirs is how many library index entries use the old id.
	MangaDirs int
	// Cache is how many cache entries use the old id (dropped when migrating).
	Cache int
}

// FindOldIDs lists the provider ids in the databases that are old ids of the loaders.
func FindOldIDs(cache, data *DB, loaders []libmangal.ProviderLoader) ([]OldID, error) {
	counts := map[string]*OldID{}
	var order []string
	add := func(id string, dirs, entries int) {
		if _, alias, ok := libmangal.FindLoader(loaders, id); !ok || alias == nil {
			return
		}
		if _, ok := counts[id]; !ok {
			loader, _, _ := libmangal.FindLoader(loaders, id)
			counts[id] = &OldID{Old: id, New: loader.Info().ID, Name: loader.Info().Name}
			order = append(order, id)
		}
		counts[id].MangaDirs += dirs
		counts[id].Cache += entries
	}

	if err := countBy(data, `SELECT provider, count(*) FROM manga_dirs GROUP BY provider`, func(id string, n int) { add(id, n, 0) }); err != nil {
		return nil, err
	}
	for _, table := range providerTables {
		if err := countBy(cache, `SELECT owner, count(*) FROM `+table+` GROUP BY owner`, func(id string, n int) { add(id, 0, n) }); err != nil {
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

// MigrateOldIDs moves the library index entries of each old id to its new id and drops
// its cache entries (they're rebuilt under the new id).
func MigrateOldIDs(cache, data *DB, oldIDs []OldID) error {
	for _, id := range oldIDs {
		if err := migrateMangaDirs(data, id); err != nil {
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

func migrateMangaDirs(data *DB, id OldID) error {
	tx, err := data.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT parent, name, info FROM manga_dirs WHERE provider = ?`, id.Old)
	if err != nil {
		return err
	}
	type row struct {
		parent, name string
		info         libmangal.MangaDirInfo
	}
	var update []row
	for rows.Next() {
		var r row
		var raw []byte
		if err := rows.Scan(&r.parent, &r.name, &raw); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal(raw, &r.info); err != nil {
			rows.Close()
			return err
		}
		update = append(update, r)
	}
	rows.Close()

	for _, r := range update {
		r.info.Provider = id.New
		raw, err := json.Marshal(r.info)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE manga_dirs SET provider = ?, info = ? WHERE parent = ? AND name = ?`, id.New, raw, r.parent, r.name); err != nil {
			return err
		}
	}
	return tx.Commit()
}

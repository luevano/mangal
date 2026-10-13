package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/luevano/libmangal/metadata"
)

// Bindings are the metadata matches of provider mangas (provider + manga id, the key of
// watches and library sources), in the data database: any number of metadata sources
// with an id each. The default (the metadata of the downloads) is the first bound source
// by priority (AniList, Kitsu, MyAnimeList, the rest), the others its backups; every one
// is used to sync the reading progress.
type Bindings struct {
	DB *DB
}

// Binding is a provider manga bound to a metadata source's manga.
type Binding struct {
	Provider string `json:"provider,omitempty"`
	MangaID  string `json:"manga_id,omitempty"`
	// Source is the metadata source's code (al, kt, mal, ...).
	Source metadata.IDCode `json:"source"`
	// ID is the metadata id, as text (AniList, Kitsu and MyAnimeList ids are integers).
	ID string `json:"id"`
	// Default is the manga's first binding by priority (worked out, not stored).
	Default bool      `json:"default"`
	Updated time.Time `json:"updated,omitzero"`
}

// MetadataID is the binding as a metadata id (as in a .libmangal.json), false for an
// unknown source.
func (b Binding) MetadataID() (metadata.ID, bool) {
	sources := map[metadata.IDCode]metadata.IDSource{
		metadata.IDCodeAnilist:      metadata.IDSourceAnilist,
		metadata.IDCodeKitsu:        metadata.IDSourceKitsu,
		metadata.IDCodeMyAnimeList:  metadata.IDSourceMyAnimeList,
		metadata.IDCodeMangaUpdates: metadata.IDSourceMangaUpdates,
		metadata.IDCodeAnimePlanet:  metadata.IDSourceAnimePlanet,
	}
	source, ok := sources[b.Source]
	if !ok || b.ID == "" {
		return metadata.ID{}, false
	}
	return metadata.ID{Raw: b.ID, Source: source, Code: b.Source}, true
}

// BindingIDs are the bindings' metadata ids (Binding.MetadataID).
func BindingIDs(bindings []Binding) []metadata.ID {
	var ids []metadata.ID
	for _, b := range bindings {
		if id, ok := b.MetadataID(); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

// sourceOrder is the priority of the sources, the first bound one is the default. Sources
// not in it come after, by code.
var sourceOrder = []metadata.IDCode{metadata.IDCodeAnilist, metadata.IDCodeKitsu, metadata.IDCodeMyAnimeList}

func sourceRank(code metadata.IDCode) int {
	if i := slices.Index(sourceOrder, code); i >= 0 {
		return i
	}
	return len(sourceOrder)
}

// SortBindings sorts a manga's bindings by priority (AniList, Kitsu, MyAnimeList, the rest by
// code) and makes the first one the default.
func SortBindings(bindings []Binding) {
	slices.SortStableFunc(bindings, func(a, b Binding) int {
		switch {
		case sourceRank(a.Source) != sourceRank(b.Source):
			return sourceRank(a.Source) - sourceRank(b.Source)
		case a.Source < b.Source:
			return -1
		case a.Source > b.Source:
			return 1
		}
		return 0
	})
	for i := range bindings {
		bindings[i].Default = i == 0
	}
}

// migration 6: the watches' anilist_id become default AniList bindings.
const bindingsTable = `CREATE TABLE metadata_bindings (
	provider    TEXT NOT NULL,
	manga_id    TEXT NOT NULL,
	source      TEXT NOT NULL,
	metadata_id TEXT NOT NULL,
	is_default  INTEGER NOT NULL DEFAULT 0,
	updated     INTEGER NOT NULL,
	PRIMARY KEY (provider, manga_id, source)
);
CREATE UNIQUE INDEX metadata_bindings_default ON metadata_bindings (provider, manga_id) WHERE is_default;
INSERT INTO metadata_bindings (provider, manga_id, source, metadata_id, is_default, updated)
	SELECT provider, manga_id, 'al', CAST(anilist_id AS TEXT), 1, added FROM watches WHERE anilist_id > 0;
ALTER TABLE watches DROP COLUMN anilist_id`

// migration 8: the default is worked out from the priority.
const bindingsNoDefault = `DROP INDEX metadata_bindings_default;
ALTER TABLE metadata_bindings DROP COLUMN is_default`

func scanBinding(rows *sql.Rows) (Binding, error) {
	var b Binding
	var updated int64
	err := rows.Scan(&b.Provider, &b.MangaID, &b.Source, &b.ID, &updated)
	b.Updated = fromUnix(updated)
	return b, err
}

const bindingColumns = `provider, manga_id, source, metadata_id, updated`

// List lists the bindings of a provider and manga, empty for any, by provider and manga
// (each manga's sorted with SortBindings).
func (s Bindings) List(provider, mangaID string) ([]Binding, error) {
	bindings, err := queryAll(s.DB, scanBinding, `SELECT `+bindingColumns+` FROM metadata_bindings
WHERE (?1 = '' OR provider = ?1) AND (?2 = '' OR manga_id = ?2) ORDER BY provider, manga_id`, provider, mangaID)
	if err != nil {
		return nil, fmt.Errorf("list metadata bindings: %w", err)
	}
	sortGrouped(bindings)
	return bindings, nil
}

// sortGrouped sorts each manga's run of bindings (they come grouped by provider and manga).
func sortGrouped(bindings []Binding) {
	start := 0
	for i := 1; i <= len(bindings); i++ {
		if i == len(bindings) || bindings[i].Provider != bindings[start].Provider || bindings[i].MangaID != bindings[start].MangaID {
			SortBindings(bindings[start:i])
			start = i
		}
	}
}

// Get is the manga's bindings, sorted with SortBindings.
func (s Bindings) Get(provider, mangaID string) ([]Binding, error) {
	if provider == "" || mangaID == "" {
		return nil, nil
	}
	return s.List(provider, mangaID)
}

// Default is the manga's default binding (the first by priority), false if it has none.
func (s Bindings) Default(provider, mangaID string) (Binding, bool, error) {
	bindings, err := s.Get(provider, mangaID)
	if err != nil || len(bindings) == 0 {
		return Binding{}, false, err
	}
	return bindings[0], true, nil
}

// Set binds the manga to b.Source's manga b.ID, replacing the source's binding (b.Default
// is ignored).
func (s Bindings) Set(b Binding) error {
	if b.Provider == "" || b.MangaID == "" || b.Source == "" || b.ID == "" {
		return fmt.Errorf("bind %s %q: incomplete binding %+v", b.Provider, b.MangaID, b)
	}
	if b.Updated.IsZero() {
		b.Updated = time.Now()
	}
	err := s.DB.InTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO metadata_bindings (provider, manga_id, source, metadata_id, updated) VALUES (?, ?, ?, ?, ?)
ON CONFLICT (provider, manga_id, source) DO UPDATE SET metadata_id = excluded.metadata_id, updated = excluded.updated`,
			b.Provider, b.MangaID, b.Source, b.ID, unix(b.Updated))
		if err != nil {
			return err
		}
		return clearSourcePicks(tx, b.Provider, b.MangaID, b.Source)
	})
	if err != nil {
		return fmt.Errorf("bind %s %q to %s %s: %w", b.Provider, b.MangaID, b.Source, b.ID, err)
	}
	return nil
}

// Remove unbinds the manga from source, false if it wasn't bound.
func (s Bindings) Remove(provider, mangaID string, source metadata.IDCode) (bool, error) {
	res, err := s.DB.Exec(`DELETE FROM metadata_bindings WHERE provider = ? AND manga_id = ? AND source = ?`, provider, mangaID, source)
	if err != nil {
		return false, fmt.Errorf("unbind %s %q from %s: %w", provider, mangaID, source, err)
	}
	n, _ := res.RowsAffected()
	return n != 0, nil
}

// OfLibraryManga is the bindings of the library manga's sources, then its own metadata
// ids (.libmangal.json) that none of them has, as non-default bindings without a provider.
func (s Bindings) OfLibraryManga(id int64) ([]Binding, error) {
	bindings, err := queryAll(s.DB, scanBinding, `SELECT b.provider, b.manga_id, b.source, b.metadata_id, b.updated
FROM metadata_bindings b JOIN library_sources s ON s.provider = b.provider AND s.manga_id = b.manga_id
WHERE s.manga = ? ORDER BY b.provider, b.manga_id`, id)
	if err != nil {
		return nil, fmt.Errorf("metadata bindings of library manga %d: %w", id, err)
	}
	sortGrouped(bindings)

	var idsJSON string
	if err := s.DB.QueryRow(`SELECT metadata_ids FROM library_mangas WHERE id = ?`, id).Scan(&idsJSON); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("metadata ids of library manga %d: %w", id, err)
	}
	var ids []metadata.ID
	if idsJSON != "" {
		if err := json.Unmarshal([]byte(idsJSON), &ids); err != nil {
			return nil, fmt.Errorf("metadata ids of library manga %d: %w", id, err)
		}
	}
	for _, mid := range ids {
		if mid.Code == "" || mid.Raw == "" || mid.Source == metadata.IDSourceProvider {
			continue
		}
		if slices.ContainsFunc(bindings, func(b Binding) bool { return b.Source == mid.Code && b.ID == mid.Raw }) {
			continue
		}
		bindings = append(bindings, Binding{Source: mid.Code, ID: mid.Raw})
	}
	return bindings, nil
}

// migrateBindings moves the bindings of an old provider id to the new one, a source bound
// under both keeps the new id's binding.
func migrateBindings(tx *sql.Tx, oldID, newID string) error {
	if _, err := tx.Exec(`DELETE FROM metadata_bindings WHERE provider = ?1 AND EXISTS (SELECT 1 FROM metadata_bindings n
	WHERE n.provider = ?2 AND n.manga_id = metadata_bindings.manga_id AND n.source = metadata_bindings.source)`, oldID, newID); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE metadata_bindings SET provider = ? WHERE provider = ?`, newID, oldID)
	return err
}

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
// with an id each, one of them the default (the metadata of the downloads), every one
// used to sync the reading progress.
type Bindings struct {
	DB *DB
}

// ErrBindingNotFound is a source the manga isn't bound to.
var ErrBindingNotFound = errors.New("binding not found")

// Binding is a provider manga bound to a metadata source's manga.
type Binding struct {
	Provider string `json:"provider,omitempty"`
	MangaID  string `json:"manga_id,omitempty"`
	// Source is the metadata source's code (al, kt, mal, ...).
	Source metadata.IDCode `json:"source"`
	// ID is the metadata id, as text (AniList, Kitsu and MyAnimeList ids are integers).
	ID      string    `json:"id"`
	Default bool      `json:"default"`
	Updated time.Time `json:"updated,omitzero"`
}

// sourceOrder is the order of the bindings of a manga after the default, and which one
// becomes the default when it's removed.
var sourceOrder = []metadata.IDCode{metadata.IDCodeAnilist, metadata.IDCodeKitsu, metadata.IDCodeMyAnimeList}

func sourceRank(code metadata.IDCode) int {
	if i := slices.Index(sourceOrder, code); i >= 0 {
		return i
	}
	return len(sourceOrder)
}

// SortBindings puts the default first, then AniList, Kitsu, MyAnimeList and the rest by code.
func SortBindings(bindings []Binding) {
	slices.SortStableFunc(bindings, func(a, b Binding) int {
		switch {
		case a.Default != b.Default:
			if a.Default {
				return -1
			}
			return 1
		case sourceRank(a.Source) != sourceRank(b.Source):
			return sourceRank(a.Source) - sourceRank(b.Source)
		case a.Source < b.Source:
			return -1
		case a.Source > b.Source:
			return 1
		}
		return 0
	})
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

func scanBinding(rows *sql.Rows) (Binding, error) {
	var b Binding
	var updated int64
	err := rows.Scan(&b.Provider, &b.MangaID, &b.Source, &b.ID, &b.Default, &updated)
	b.Updated = fromUnix(updated)
	return b, err
}

const bindingColumns = `provider, manga_id, source, metadata_id, is_default, updated`

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

// Default is the manga's default binding, false if it has none.
func (s Bindings) Default(provider, mangaID string) (Binding, bool, error) {
	bindings, err := queryAll(s.DB, scanBinding, `SELECT `+bindingColumns+` FROM metadata_bindings
WHERE provider = ? AND manga_id = ? AND is_default`, provider, mangaID)
	if err != nil {
		return Binding{}, false, fmt.Errorf("default metadata binding of %s %q: %w", provider, mangaID, err)
	}
	if len(bindings) == 0 {
		return Binding{}, false, nil
	}
	return bindings[0], true, nil
}

// Set binds the manga to b.Source's manga b.ID, replacing the source's binding. It's the
// default when b.Default or when the manga has no other binding; a binding that was the
// default stays it.
func (s Bindings) Set(b Binding) error {
	if b.Provider == "" || b.MangaID == "" || b.Source == "" || b.ID == "" {
		return fmt.Errorf("bind %s %q: incomplete binding %+v", b.Provider, b.MangaID, b)
	}
	if b.Updated.IsZero() {
		b.Updated = time.Now()
	}
	err := s.DB.InTx(func(tx *sql.Tx) error {
		if b.Default {
			if _, err := tx.Exec(`UPDATE metadata_bindings SET is_default = 0 WHERE provider = ? AND manga_id = ? AND source <> ? AND is_default`,
				b.Provider, b.MangaID, b.Source); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`INSERT INTO metadata_bindings (provider, manga_id, source, metadata_id, is_default, updated)
VALUES (?1, ?2, ?3, ?4, ?5 OR NOT EXISTS (SELECT 1 FROM metadata_bindings WHERE provider = ?1 AND manga_id = ?2 AND is_default AND source <> ?3), ?6)
ON CONFLICT (provider, manga_id, source) DO UPDATE SET metadata_id = excluded.metadata_id,
	is_default = is_default OR excluded.is_default, updated = excluded.updated`,
			b.Provider, b.MangaID, b.Source, b.ID, b.Default, unix(b.Updated)); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("bind %s %q to %s %s: %w", b.Provider, b.MangaID, b.Source, b.ID, err)
	}
	return nil
}

// SetDefault makes the manga's binding of source the default, ErrBindingNotFound if it
// has none.
func (s Bindings) SetDefault(provider, mangaID string, source metadata.IDCode) error {
	err := s.DB.InTx(func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRow(`SELECT count(*) FROM metadata_bindings WHERE provider = ? AND manga_id = ? AND source = ?`,
			provider, mangaID, source).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return ErrBindingNotFound
		}
		if _, err := tx.Exec(`UPDATE metadata_bindings SET is_default = 0 WHERE provider = ? AND manga_id = ? AND is_default`, provider, mangaID); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE metadata_bindings SET is_default = 1, updated = ? WHERE provider = ? AND manga_id = ? AND source = ?`,
			time.Now().Unix(), provider, mangaID, source)
		return err
	})
	if err != nil {
		return fmt.Errorf("set the default metadata of %s %q to %s: %w", provider, mangaID, source, err)
	}
	return nil
}

// Remove unbinds the manga from source, false if it wasn't bound. When it was the default
// the next binding (AniList, Kitsu, MyAnimeList, the rest) becomes it.
func (s Bindings) Remove(provider, mangaID string, source metadata.IDCode) (bool, error) {
	removed := false
	err := s.DB.InTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`DELETE FROM metadata_bindings WHERE provider = ? AND manga_id = ? AND source = ?`, provider, mangaID, source)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		removed = n != 0
		return promoteDefault(tx, provider, mangaID)
	})
	if err != nil {
		return false, fmt.Errorf("unbind %s %q from %s: %w", provider, mangaID, source, err)
	}
	return removed, nil
}

// promoteDefault makes a binding of the manga the default when none is.
func promoteDefault(tx *sql.Tx, provider, mangaID string) error {
	left, err := queryAll(tx, scanBinding, `SELECT `+bindingColumns+` FROM metadata_bindings WHERE provider = ? AND manga_id = ?`, provider, mangaID)
	if err != nil || len(left) == 0 {
		return err
	}
	SortBindings(left)
	if left[0].Default {
		return nil
	}
	_, err = tx.Exec(`UPDATE metadata_bindings SET is_default = 1 WHERE provider = ? AND manga_id = ? AND source = ?`, provider, mangaID, left[0].Source)
	return err
}

// OfLibraryManga is the bindings of the library manga's sources, then its own metadata
// ids (.libmangal.json) that none of them has, as non-default bindings without a provider.
func (s Bindings) OfLibraryManga(id int64) ([]Binding, error) {
	bindings, err := queryAll(s.DB, scanBinding, `SELECT b.provider, b.manga_id, b.source, b.metadata_id, b.is_default, b.updated
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
// under both keeps the new id's binding (and its default).
func migrateBindings(tx *sql.Tx, oldID, newID string) error {
	if _, err := tx.Exec(`DELETE FROM metadata_bindings WHERE provider = ?1 AND EXISTS (SELECT 1 FROM metadata_bindings n
	WHERE n.provider = ?2 AND n.manga_id = metadata_bindings.manga_id AND n.source = metadata_bindings.source)`, oldID, newID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE metadata_bindings SET is_default = 0 WHERE provider = ?1 AND is_default AND EXISTS (SELECT 1 FROM metadata_bindings n
	WHERE n.provider = ?2 AND n.manga_id = metadata_bindings.manga_id AND n.is_default)`, oldID, newID); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE metadata_bindings SET provider = ? WHERE provider = ?`, newID, oldID)
	return err
}

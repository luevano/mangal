package storage

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/luevano/libmangal"
)

var _ libmangal.MangaDirIndex = MangaDirs{}

// MangaDirs is the library index: where each manga was downloaded.
type MangaDirs struct {
	DB *DB
}

func (m MangaDirs) FindMangaDir(parent string, provider libmangal.ProviderInfo, info libmangal.MangaDirInfo) (string, bool, error) {
	rows, err := m.DB.Query(`SELECT name, info FROM manga_dirs WHERE parent = ?`, parent)
	if err != nil {
		return "", false, fmt.Errorf("find manga dir in %s: %w", parent, err)
	}
	defer rows.Close()

	best, name := libmangal.MangaDirNoMatch, ""
	for rows.Next() {
		var rowName string
		var data []byte
		if err := rows.Scan(&rowName, &data); err != nil {
			return "", false, err
		}
		var have libmangal.MangaDirInfo
		if err := json.Unmarshal(data, &have); err != nil {
			return "", false, fmt.Errorf("manga dir %q: %w", rowName, err)
		}
		if match := libmangal.MatchMangaDir(provider, info, have); match > best {
			best, name = match, rowName
		}
	}
	if err := rows.Err(); err != nil {
		return "", false, err
	}
	return name, best != libmangal.MangaDirNoMatch, nil
}

func (m MangaDirs) RememberMangaDir(parent, name string, info libmangal.MangaDirInfo) error {
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	_, err = m.DB.Exec(`INSERT INTO manga_dirs (parent, name, provider, manga_id, info, updated) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (parent, name) DO UPDATE SET provider = excluded.provider, manga_id = excluded.manga_id, info = excluded.info, updated = excluded.updated`,
		parent, name, info.Provider, info.Manga.ID, data, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("remember manga dir %q: %w", name, err)
	}
	return nil
}

// Moved updates the index after a directory was renamed from one path to another:
// a manga directory, or a directory with manga directories (e.g. a provider's).
func (m MangaDirs) Moved(from, to string) error {
	_, err := m.DB.Exec(`UPDATE manga_dirs SET parent = ?, name = ? WHERE parent = ? AND name = ?`,
		filepath.Dir(to), filepath.Base(to), filepath.Dir(from), filepath.Base(from))
	if err == nil {
		_, err = m.DB.Exec(`UPDATE manga_dirs SET parent = ? WHERE parent = ?`, to, from)
	}
	if err == nil {
		// the downloaded chapters under the directory
		_, err = m.DB.Exec(`UPDATE downloads SET path = ? || substr(path, ?) WHERE substr(path, 1, ?) = ?`,
			to, len(from)+1, len(from)+1, from+string(filepath.Separator))
	}
	if err != nil {
		return fmt.Errorf("move %s in the library index: %w", from, err)
	}
	return nil
}

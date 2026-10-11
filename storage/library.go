package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
)

var (
	_ libmangal.MangaDirIndex = Library{}
	_ libmangal.ChapterIndex  = Library{}
)

// Library is what's downloaded: each manga is one folder (parent/dir, dir empty if the
// chapters are in parent directly), linked to the provider mangas downloaded into it
// (sources), with its chapters by number and the downloaded files.
type Library struct {
	DB *DB
}

// Manga is a manga of the library.
type Manga struct {
	ID          int64
	Title       string
	Parent, Dir string
	MetadataIDs []metadata.ID
	Sources     []Source
	Chapters    int
}

// Path is the manga's folder.
func (m Manga) Path() string {
	return filepath.Join(m.Parent, m.Dir)
}

// Source is a provider's manga downloaded into a library manga.
type Source struct {
	Provider, MangaID, Title string
}

// LibraryChapter is a chapter of a library manga, the same for every provider.
type LibraryChapter struct {
	ID     int64
	Manga  int64
	Number float32
	Volume float32
	// Name is the chapter's title when a source has one, may be empty.
	Name string
}

const libraryTables = `
CREATE TABLE library_mangas (
	id           INTEGER PRIMARY KEY,
	title        TEXT NOT NULL,
	parent       TEXT NOT NULL,
	dir          TEXT NOT NULL,
	metadata_ids TEXT NOT NULL DEFAULT '[]',
	UNIQUE (parent, dir)
);
CREATE TABLE library_sources (
	provider TEXT NOT NULL,
	manga_id TEXT NOT NULL,
	manga    INTEGER NOT NULL REFERENCES library_mangas (id) ON DELETE CASCADE,
	title    TEXT NOT NULL,
	PRIMARY KEY (provider, manga_id)
);
CREATE INDEX library_sources_manga ON library_sources (manga);
CREATE TABLE library_chapters (
	id     INTEGER PRIMARY KEY,
	manga  INTEGER NOT NULL REFERENCES library_mangas (id) ON DELETE CASCADE,
	number REAL NOT NULL,
	volume REAL NOT NULL DEFAULT 0,
	name   TEXT NOT NULL DEFAULT '',
	UNIQUE (manga, number)
);
ALTER TABLE downloads ADD COLUMN chapter_id INTEGER REFERENCES library_chapters (id) ON DELETE CASCADE;
CREATE INDEX downloads_chapter ON downloads (chapter_id);
`

// migrateLibrary replaces manga_dirs with the library tables and links the downloads.
func migrateLibrary(tx *sql.Tx) error {
	if _, err := tx.Exec(libraryTables); err != nil {
		return err
	}

	rows, err := tx.Query(`SELECT parent, name, info FROM manga_dirs`)
	if err != nil {
		return err
	}
	type dir struct {
		parent, name string
		info         libmangal.MangaDirInfo
	}
	var dirs []dir
	for rows.Next() {
		var d dir
		var raw []byte
		if err := rows.Scan(&d.parent, &d.name, &raw); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal(raw, &d.info); err != nil {
			rows.Close()
			return err
		}
		dirs = append(dirs, d)
	}
	rows.Close()
	for _, d := range dirs {
		if err := rememberDir(tx, d.parent, d.name, d.info); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DROP TABLE manga_dirs`); err != nil {
		return err
	}

	rows, err = tx.Query(`SELECT path, provider, manga_id, manga_title, volume, chapter, chapter_title FROM downloads`)
	if err != nil {
		return err
	}
	var downloads []Download
	for rows.Next() {
		var dl Download
		if err := rows.Scan(&dl.Path, &dl.Provider, &dl.MangaID, &dl.MangaTitle, &dl.Volume, &dl.Chapter, &dl.ChapterTitle); err != nil {
			rows.Close()
			return err
		}
		downloads = append(downloads, dl)
	}
	rows.Close()
	for _, dl := range downloads {
		// the chapter's folder is (usually) the manga's
		folder := filepath.Dir(dl.Path)
		manga, err := mangaFor(tx, filepath.Dir(folder), filepath.Base(folder), Source{Provider: dl.Provider, MangaID: dl.MangaID, Title: dl.MangaTitle})
		if err != nil {
			return err
		}
		chapter, err := upsertChapter(tx, manga, dl.Chapter, dl.Volume, dl.ChapterTitle)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE downloads SET chapter_id = ? WHERE path = ?`, chapter, dl.Path); err != nil {
			return err
		}
	}
	return nil
}

// FindMangaDir implements libmangal.MangaDirIndex.
func (l Library) FindMangaDir(parent string, provider libmangal.ProviderInfo, info libmangal.MangaDirInfo) (string, bool, error) {
	mangas, err := l.mangas(`WHERE m.parent = ? AND m.dir != ''`, parent)
	if err != nil {
		return "", false, fmt.Errorf("find manga dir in %s: %w", parent, err)
	}
	best, name := libmangal.MangaDirNoMatch, ""
	for _, m := range mangas {
		// a folder found by a scan has no source until it's downloaded into
		haves := []libmangal.MangaDirInfo{{MetadataIDs: m.MetadataIDs}}
		for _, src := range m.Sources {
			haves = append(haves, libmangal.MangaDirInfo{Provider: src.Provider, Manga: mangadata.MangaInfo{ID: src.MangaID}, MetadataIDs: m.MetadataIDs})
		}
		for _, have := range haves {
			if match := libmangal.MatchMangaDir(provider, info, have); match > best {
				best, name = match, m.Dir
			}
		}
	}
	return name, best != libmangal.MangaDirNoMatch, nil
}

// RememberMangaDir implements libmangal.MangaDirIndex.
func (l Library) RememberMangaDir(parent, name string, info libmangal.MangaDirInfo) error {
	return l.inTx(func(tx *sql.Tx) error {
		return rememberDir(tx, parent, name, info)
	})
}

func rememberDir(tx *sql.Tx, parent, name string, info libmangal.MangaDirInfo) error {
	manga, err := mangaFor(tx, parent, name, Source{Provider: info.Provider, MangaID: info.Manga.ID, Title: info.Manga.Title})
	if err != nil {
		return fmt.Errorf("remember manga dir %q: %w", name, err)
	}
	return addMetadataIDs(tx, manga, info.MetadataIDs)
}

// mangaFor is the library manga of the folder (created if new), the source now points to it.
func mangaFor(tx *sql.Tx, parent, dir string, src Source) (int64, error) {
	var id int64
	err := tx.QueryRow(`SELECT id FROM library_mangas WHERE parent = ? AND dir = ?`, parent, dir).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		res, err := tx.Exec(`INSERT INTO library_mangas (title, parent, dir) VALUES (?, ?, ?)`, src.Title, parent, dir)
		if err != nil {
			return 0, err
		}
		id, err = res.LastInsertId()
		if err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, err
	}
	// without a manga id (a folder found by a scan) there's nothing to link yet
	if src.Provider == "" || src.MangaID == "" {
		return id, nil
	}
	_, err = tx.Exec(`INSERT INTO library_sources (provider, manga_id, manga, title) VALUES (?, ?, ?, ?)
ON CONFLICT (provider, manga_id) DO UPDATE SET manga = excluded.manga, title = excluded.title`, src.Provider, src.MangaID, id, src.Title)
	return id, err
}

func addMetadataIDs(tx *sql.Tx, manga int64, ids []metadata.ID) error {
	if len(ids) == 0 {
		return nil
	}
	var raw string
	if err := tx.QueryRow(`SELECT metadata_ids FROM library_mangas WHERE id = ?`, manga).Scan(&raw); err != nil {
		return err
	}
	var have []metadata.ID
	if err := json.Unmarshal([]byte(raw), &have); err != nil {
		return err
	}
	for _, id := range ids {
		if !slices.Contains(have, id) {
			have = append(have, id)
		}
	}
	data, err := json.Marshal(have)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE library_mangas SET metadata_ids = ? WHERE id = ?`, string(data), manga)
	return err
}

// upsertChapter returns the chapter of the manga with the number, keeping a known name.
func upsertChapter(tx *sql.Tx, manga int64, number, volume float32, name string) (int64, error) {
	var id int64
	err := tx.QueryRow(`INSERT INTO library_chapters (manga, number, volume, name) VALUES (?, ?, ?, ?)
ON CONFLICT (manga, number) DO UPDATE SET volume = excluded.volume,
	name = CASE excluded.name WHEN '' THEN name ELSE excluded.name END
RETURNING id`, manga, number, volume, name).Scan(&id)
	return id, err
}

// FindChapter implements libmangal.ChapterIndex: a download of the chapter number in the
// format, in the manga folder (or, for chapters without a manga folder, in mangaDir itself).
func (l Library) FindChapter(mangaDir string, chapter mangadata.Chapter, format libmangal.Format) (string, string, bool, error) {
	var path, provider string
	err := l.DB.QueryRow(`SELECT d.path, d.provider FROM downloads d
JOIN library_chapters c ON c.id = d.chapter_id
JOIN library_mangas m ON m.id = c.manga
WHERE ((m.parent = ? AND m.dir = ?) OR (m.parent = ? AND m.dir = '')) AND c.number = ? AND d.format = ?
LIMIT 1`, filepath.Dir(mangaDir), filepath.Base(mangaDir), mangaDir, chapter.Info().Number, format.String()).Scan(&path, &provider)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("find chapter %g: %w", chapter.Info().Number, err)
	}
	return path, provider, true, nil
}

// RecordDownload records a downloaded chapter of the manga folder.
func (l Library) RecordDownload(parent, dir string, dl Download) error {
	err := l.inTx(func(tx *sql.Tx) error {
		manga, err := mangaFor(tx, parent, dir, Source{Provider: dl.Provider, MangaID: dl.MangaID, Title: dl.MangaTitle})
		if err != nil {
			return err
		}
		if dl.ChapterID, err = upsertChapter(tx, manga, dl.Chapter, dl.Volume, dl.ChapterTitle); err != nil {
			return err
		}
		return recordDownload(tx, dl)
	})
	if err != nil {
		return fmt.Errorf("record download %s: %w", dl.Path, err)
	}
	return nil
}

// HasDownload reports whether the file is recorded.
func (l Library) HasDownload(path string) (bool, error) {
	var n int
	err := l.DB.QueryRow(`SELECT count(*) FROM downloads WHERE path = ?`, path).Scan(&n)
	return n > 0, err
}

// Mangas lists the library, by title.
func (l Library) Mangas() ([]Manga, error) {
	return l.mangas(`ORDER BY m.title COLLATE NOCASE`)
}

// MangaByID is the library manga with the id.
func (l Library) MangaByID(id int64) (Manga, bool, error) {
	mangas, err := l.mangas(`WHERE m.id = ?`, id)
	if err != nil || len(mangas) == 0 {
		return Manga{}, false, err
	}
	return mangas[0], true, nil
}

func (l Library) mangas(where string, args ...any) ([]Manga, error) {
	rows, err := l.DB.Query(`SELECT m.id, m.title, m.parent, m.dir, m.metadata_ids,
	(SELECT count(*) FROM library_chapters c WHERE c.manga = m.id AND EXISTS (SELECT 1 FROM downloads d WHERE d.chapter_id = c.id))
FROM library_mangas m `+where, args...)
	if err != nil {
		return nil, err
	}
	var mangas []Manga
	for rows.Next() {
		var m Manga
		var ids string
		if err := rows.Scan(&m.ID, &m.Title, &m.Parent, &m.Dir, &ids, &m.Chapters); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal([]byte(ids), &m.MetadataIDs); err != nil {
			rows.Close()
			return nil, err
		}
		mangas = append(mangas, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range mangas {
		if mangas[i].Sources, err = l.sources(mangas[i].ID); err != nil {
			return nil, err
		}
	}
	return mangas, nil
}

func (l Library) sources(manga int64) ([]Source, error) {
	rows, err := l.DB.Query(`SELECT provider, manga_id, title FROM library_sources WHERE manga = ? ORDER BY provider`, manga)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sources []Source
	for rows.Next() {
		var s Source
		if err := rows.Scan(&s.Provider, &s.MangaID, &s.Title); err != nil {
			return nil, err
		}
		sources = append(sources, s)
	}
	return sources, rows.Err()
}

// Downloads lists the downloads of a library manga, by chapter.
func (l Library) Downloads(manga int64) ([]Download, error) {
	return queryDownloads(l.DB, `WHERE c.manga = ? ORDER BY c.number, d.format, d.path`, manga)
}

// Moved updates the library after a directory was renamed from one path to another:
// a manga folder, or a directory with manga folders (e.g. a provider's).
func (l Library) Moved(from, to string) error {
	err := l.inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE library_mangas SET parent = ?, dir = ? WHERE parent = ? AND dir = ?`,
			filepath.Dir(to), filepath.Base(to), filepath.Dir(from), filepath.Base(from)); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE library_mangas SET parent = ? WHERE parent = ?`, to, from); err != nil {
			return err
		}
		// substr and length count characters, not bytes
		_, err := tx.Exec(`UPDATE downloads SET path = ?1 || substr(path, length(?2)) WHERE substr(path, 1, length(?2)) = ?2`,
			to, from+string(filepath.Separator))
		return err
	})
	if err != nil {
		return fmt.Errorf("move %s in the library: %w", from, err)
	}
	return nil
}

// MangaByFolder is the library manga of the folder.
func (l Library) MangaByFolder(parent, dir string) (Manga, bool, error) {
	mangas, err := l.mangas(`WHERE m.parent = ? AND m.dir = ?`, parent, dir)
	if err != nil || len(mangas) == 0 {
		return Manga{}, false, err
	}
	return mangas[0], true, nil
}

// MangaBySource is the library manga a provider's manga was last downloaded into.
func (l Library) MangaBySource(provider, mangaID string) (Manga, bool, error) {
	mangas, err := l.mangas(`WHERE m.id = (SELECT manga FROM library_sources WHERE provider = ? AND manga_id = ?)`, provider, mangaID)
	if err != nil || len(mangas) == 0 {
		return Manga{}, false, err
	}
	return mangas[0], true, nil
}

// SetDownloadPath updates the path of a recorded file (it was moved).
func (l Library) SetDownloadPath(from, to string) error {
	if _, err := l.DB.Exec(`UPDATE downloads SET path = ? WHERE path = ?`, to, from); err != nil {
		return fmt.Errorf("move download %s: %w", from, err)
	}
	return nil
}

// RemoveDownload forgets a recorded file, and its chapter if it has no other file.
func (l Library) RemoveDownload(path string) error {
	err := l.inTx(func(tx *sql.Tx) error {
		var chapter sql.NullInt64
		if err := tx.QueryRow(`SELECT chapter_id FROM downloads WHERE path = ?`, path).Scan(&chapter); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		if _, err := tx.Exec(`DELETE FROM downloads WHERE path = ?`, path); err != nil {
			return err
		}
		if chapter.Valid {
			_, err := tx.Exec(`DELETE FROM library_chapters WHERE id = ? AND NOT EXISTS (SELECT 1 FROM downloads WHERE chapter_id = ?)`, chapter.Int64, chapter.Int64)
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("remove download %s: %w", path, err)
	}
	return nil
}

// RemoveManga forgets a library manga, with its sources, chapters and downloads.
func (l Library) RemoveManga(id int64) error {
	err := l.inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM downloads WHERE chapter_id IN (SELECT id FROM library_chapters WHERE manga = ?)`, id); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM library_mangas WHERE id = ?`, id)
		return err
	})
	if err != nil {
		return fmt.Errorf("remove manga %d: %w", id, err)
	}
	return nil
}

// MergeMove is a file of the merged manga moved into the kept one.
type MergeMove struct {
	From, To string
}

// Merge joins other into keep: the moved files get keep's chapters, the trashed files
// (chapters keep already had) are forgotten, other's sources and metadata ids go to keep
// and other is removed.
func (l Library) Merge(keep, other int64, moved []MergeMove, trashed []string) error {
	err := l.inTx(func(tx *sql.Tx) error {
		for _, path := range trashed {
			if _, err := tx.Exec(`DELETE FROM downloads WHERE path = ?`, path); err != nil {
				return err
			}
		}
		for _, m := range moved {
			var number, volume float32
			var name string
			err := tx.QueryRow(`SELECT c.number, c.volume, c.name FROM downloads d JOIN library_chapters c ON c.id = d.chapter_id WHERE d.path = ?`, m.From).Scan(&number, &volume, &name)
			if err != nil {
				return fmt.Errorf("%s: %w", m.From, err)
			}
			chapter, err := upsertChapter(tx, keep, number, volume, name)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE downloads SET path = ?, chapter_id = ? WHERE path = ?`, m.To, chapter, m.From); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`UPDATE library_sources SET manga = ? WHERE manga = ?`, keep, other); err != nil {
			return err
		}
		var raw string
		if err := tx.QueryRow(`SELECT metadata_ids FROM library_mangas WHERE id = ?`, other).Scan(&raw); err != nil {
			return err
		}
		var ids []metadata.ID
		if err := json.Unmarshal([]byte(raw), &ids); err != nil {
			return err
		}
		if err := addMetadataIDs(tx, keep, ids); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM library_mangas WHERE id = ?`, other)
		return err
	})
	if err != nil {
		return fmt.Errorf("merge manga %d into %d: %w", other, keep, err)
	}
	return nil
}

func (l Library) inTx(f func(*sql.Tx) error) error {
	tx, err := l.DB.Begin()
	if err != nil {
		return err
	}
	if err := f(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

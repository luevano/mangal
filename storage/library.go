package storage

import (
	"cmp"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"time"

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
	// Status is the publication status of its AniList manga (metadata.Status), "" when
	// unknown; StatusUpdated is when it was looked up.
	Status        string
	StatusUpdated time.Time
	// Hold is a mark that its watches are paused for (HoldHiatus, HoldEnded), "" for none.
	Hold string
	// NewProvider is why it needs another provider (marked by hand, for a manga no watch
	// has: a watch's is its re-link flag), "" when it doesn't.
	NewProvider string
}

// Path is the manga's folder.
func (m Manga) Path() string {
	return filepath.Join(m.Parent, m.Dir)
}

// Source is a provider's manga downloaded into a library manga.
type Source struct {
	Provider, MangaID, Title string
	// Moved is when a move (Library.Move) made it the manga's source, zero if none did.
	Moved time.Time
	// Current is the source the manga's folder follows now: the one moved to or downloaded
	// from last (what its .libmangal.json says), the others are previous ones. Mangas lists
	// it first.
	Current bool
}

// sourceMoved: when a move made a source the library manga's.
const sourceMoved = `ALTER TABLE library_sources ADD COLUMN moved INTEGER NOT NULL DEFAULT 0`

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

	type dir struct {
		parent, name string
		info         libmangal.MangaDirInfo
	}
	dirs, err := queryAll(tx, func(rows *sql.Rows) (dir, error) {
		var d dir
		var raw []byte
		if err := rows.Scan(&d.parent, &d.name, &raw); err != nil {
			return d, err
		}
		return d, json.Unmarshal(raw, &d.info)
	}, `SELECT parent, name, info FROM manga_dirs`)
	if err != nil {
		return err
	}
	for _, d := range dirs {
		if err := rememberDir(tx, d.parent, d.name, d.info); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DROP TABLE manga_dirs`); err != nil {
		return err
	}

	downloads, err := queryAll(tx, func(rows *sql.Rows) (Download, error) {
		var dl Download
		err := rows.Scan(&dl.Path, &dl.Provider, &dl.MangaID, &dl.MangaTitle, &dl.Volume, &dl.Chapter, &dl.ChapterTitle)
		return dl, err
	}, `SELECT path, provider, manga_id, manga_title, volume, chapter, chapter_title FROM downloads`)
	if err != nil {
		return err
	}
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
	type row struct {
		id       int64
		dir, ids string
		src      Source
	}
	// a folder found by a scan has no source until it's downloaded into (LEFT JOIN)
	rows, err := queryAll(l.DB, func(rows *sql.Rows) (row, error) {
		var r row
		return r, rows.Scan(&r.id, &r.dir, &r.ids, &r.src.Provider, &r.src.MangaID)
	}, `SELECT m.id, m.dir, m.metadata_ids, coalesce(s.provider, ''), coalesce(s.manga_id, '')
FROM library_mangas m LEFT JOIN library_sources s ON s.manga = m.id
WHERE m.parent = ? AND m.dir != '' ORDER BY m.dir, m.id`, parent)
	if err != nil {
		return "", false, fmt.Errorf("find manga dir in %s: %w", parent, err)
	}
	best, name := libmangal.MangaDirNoMatch, ""
	var last int64
	var ids []metadata.ID
	for _, r := range rows {
		var haves []libmangal.MangaDirInfo
		if r.id != last {
			last, ids = r.id, nil
			if err := json.Unmarshal([]byte(r.ids), &ids); err != nil {
				return "", false, fmt.Errorf("find manga dir in %s: %w", parent, err)
			}
			haves = append(haves, libmangal.MangaDirInfo{MetadataIDs: ids})
		}
		if r.src.Provider != "" {
			haves = append(haves, libmangal.MangaDirInfo{Provider: r.src.Provider, Manga: mangadata.MangaInfo{ID: r.src.MangaID}, MetadataIDs: ids})
		}
		for _, have := range haves {
			if match := libmangal.MatchMangaDir(provider, info, have); match > best {
				best, name = match, r.dir
			}
		}
	}
	return name, best != libmangal.MangaDirNoMatch, nil
}

// RememberMangaDir implements libmangal.MangaDirIndex.
func (l Library) RememberMangaDir(parent, name string, info libmangal.MangaDirInfo) error {
	l.DB.touch()
	return l.DB.InTx(func(tx *sql.Tx) error {
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

// LinkChange changes the metadata links of a library manga: its own metadata ids and the
// bindings of provider mangas (its sources, its watches'), in one transaction.
type LinkChange struct {
	Manga int64
	// Links are the new id of each source, replacing the ones it has; "" removes them.
	Links map[metadata.IDCode]string
	// Bindings are the provider mangas (provider, manga id) whose binding of each source is
	// replaced (removed for "") too.
	Bindings [][2]string
	// From and To are the manga's folder renamed, both empty when it isn't.
	From, To string
	At       time.Time
}

// SetLinks makes the change: the old ids of a changed source stop matching anything.
func (l Library) SetLinks(c LinkChange) error {
	err := l.DB.InTx(func(tx *sql.Tx) error {
		var raw string
		if err := tx.QueryRow(`SELECT metadata_ids FROM library_mangas WHERE id = ?`, c.Manga).Scan(&raw); err != nil {
			return err
		}
		var ids []metadata.ID
		if err := json.Unmarshal([]byte(raw), &ids); err != nil {
			return err
		}
		oldAnilist := ""
		if i := slices.IndexFunc(ids, func(id metadata.ID) bool { return id.Code == metadata.IDCodeAnilist }); i >= 0 {
			oldAnilist = ids[i].Raw
		}
		ids = ReplaceIDs(ids, c.Links)
		data, err := json.Marshal(ids)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE library_mangas SET metadata_ids = ? WHERE id = ?`, string(data), c.Manga); err != nil {
			return err
		}
		if err := clearPicks(tx, c.Manga, c.Links); err != nil {
			return err
		}
		if al, ok := c.Links[metadata.IDCodeAnilist]; ok && al != oldAnilist {
			// the status was the old AniList manga's
			if _, err := tx.Exec(`UPDATE library_mangas SET status = '', status_updated = 0 WHERE id = ?`, c.Manga); err != nil {
				return err
			}
		}
		for _, b := range c.Bindings {
			for code, id := range c.Links {
				if id == "" {
					_, err = tx.Exec(`DELETE FROM metadata_bindings WHERE provider = ? AND manga_id = ? AND source = ?`, b[0], b[1], code)
				} else {
					_, err = tx.Exec(`INSERT INTO metadata_bindings (provider, manga_id, source, metadata_id, updated) VALUES (?, ?, ?, ?, ?)
ON CONFLICT (provider, manga_id, source) DO UPDATE SET metadata_id = excluded.metadata_id, updated = excluded.updated`, b[0], b[1], code, id, unix(c.At))
				}
				if err != nil {
					return err
				}
			}
		}
		if c.From != "" && c.From != c.To {
			return moved(tx, c.From, c.To)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("set the links of library manga %d: %w", c.Manga, err)
	}
	return nil
}

// ReplaceIDs is ids with each source of links replaced by its id (in the place of the
// source's first id, else at the end), or removed for "".
func ReplaceIDs(ids []metadata.ID, links map[metadata.IDCode]string) []metadata.ID {
	out := []metadata.ID{}
	done := map[metadata.IDCode]bool{}
	for _, id := range ids {
		newID, ok := links[id.Code]
		switch {
		case !ok:
			out = append(out, id)
		case !done[id.Code]:
			done[id.Code] = true
			if mid, valid := (Binding{Source: id.Code, ID: newID}).MetadataID(); valid {
				out = append(out, mid)
			}
		}
	}
	for _, code := range sourceOrder {
		if newID, ok := links[code]; ok && !done[code] {
			if mid, valid := (Binding{Source: code, ID: newID}).MetadataID(); valid {
				out = append(out, mid)
			}
			done[code] = true
		}
	}
	for code, newID := range links {
		if !done[code] {
			if mid, valid := (Binding{Source: code, ID: newID}).MetadataID(); valid {
				out = append(out, mid)
			}
		}
	}
	return out
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
	l.DB.touch()
	err := l.DB.InTx(func(tx *sql.Tx) error {
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
	mangas, err := queryAll(l.DB, func(rows *sql.Rows) (Manga, error) {
		var m Manga
		var ids string
		var updated int64
		if err := rows.Scan(&m.ID, &m.Title, &m.Parent, &m.Dir, &ids, &m.Status, &updated, &m.Hold, &m.NewProvider, &m.Chapters); err != nil {
			return m, err
		}
		m.StatusUpdated = fromUnix(updated)
		return m, json.Unmarshal([]byte(ids), &m.MetadataIDs)
	}, `SELECT m.id, m.title, m.parent, m.dir, m.metadata_ids, m.status, m.status_updated, m.hold, m.new_provider,
	(SELECT count(*) FROM library_chapters c WHERE c.manga = m.id AND EXISTS (SELECT 1 FROM downloads d WHERE d.chapter_id = c.id))
FROM library_mangas m `+where, args...)
	if err != nil || len(mangas) == 0 {
		return nil, err
	}

	byID := make(map[int64]*Manga, len(mangas))
	ids := make([]int64, len(mangas))
	for i := range mangas {
		byID[mangas[i].ID] = &mangas[i]
		ids[i] = mangas[i].ID
	}
	idsJSON, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	type source struct {
		manga int64
		// last is the later of the move and its last download
		last int64
		Source
	}
	sources, err := queryAll(l.DB, func(rows *sql.Rows) (source, error) {
		var s source
		var moved int64
		err := rows.Scan(&s.manga, &s.Provider, &s.MangaID, &s.Title, &moved, &s.last)
		s.Moved = fromUnix(moved)
		return s, err
	}, `SELECT s.manga, s.provider, s.manga_id, s.title, s.moved,
	max(s.moved, coalesce((SELECT max(d.downloaded) FROM downloads d WHERE d.provider = s.provider AND d.manga_id = s.manga_id), 0))
FROM library_sources s WHERE s.manga IN (SELECT value FROM json_each(?)) ORDER BY s.manga, s.provider`, string(idsJSON))
	if err != nil {
		return nil, err
	}
	last := map[int64]int64{}
	for _, s := range sources {
		m := byID[s.manga]
		if len(m.Sources) == 0 || s.last > last[s.manga] {
			// the current one first
			m.Sources = append([]Source{s.Source}, m.Sources...)
			last[s.manga] = s.last
		} else {
			m.Sources = append(m.Sources, s.Source)
		}
	}
	for _, m := range mangas {
		if len(m.Sources) != 0 {
			m.Sources[0].Current = true
		}
	}
	return mangas, nil
}

// Downloads lists the downloads of a library manga, by chapter.
func (l Library) Downloads(manga int64) ([]Download, error) {
	return queryDownloads(l.DB, `WHERE c.manga = ? ORDER BY c.number, d.format, d.path`, manga)
}

// ChapterNumbers are the downloaded chapter numbers of every library manga, sorted, by
// library manga.
func (l Library) ChapterNumbers() (map[int64][]float32, error) {
	type row struct {
		manga  int64
		number float32
	}
	rows, err := queryAll(l.DB, func(rows *sql.Rows) (row, error) {
		var r row
		err := rows.Scan(&r.manga, &r.number)
		return r, err
	}, `SELECT c.manga, c.number FROM library_chapters c
WHERE EXISTS (SELECT 1 FROM downloads d WHERE d.chapter_id = c.id) ORDER BY c.manga, c.number`)
	if err != nil {
		return nil, fmt.Errorf("library chapter numbers: %w", err)
	}
	numbers := map[int64][]float32{}
	for _, r := range rows {
		numbers[r.manga] = append(numbers[r.manga], r.number)
	}
	return numbers, nil
}

// ChapterHosts are the hosts of the recorded downloads' chapter URLs by provider and
// manga id: the site a manga id is from (an old provider id's site, e.g.).
func (l Library) ChapterHosts() (map[[2]string][]string, error) {
	type row struct{ provider, mangaID, url string }
	rows, err := queryAll(l.DB, func(rows *sql.Rows) (row, error) {
		var r row
		err := rows.Scan(&r.provider, &r.mangaID, &r.url)
		return r, err
	}, `SELECT DISTINCT provider, manga_id, chapter_url FROM downloads WHERE provider != '' AND manga_id != '' AND chapter_url != ''`)
	if err != nil {
		return nil, fmt.Errorf("library chapter urls: %w", err)
	}
	hosts := map[[2]string][]string{}
	for _, r := range rows {
		u, err := url.Parse(r.url)
		if err != nil || u.Host == "" {
			continue
		}
		k := [2]string{r.provider, r.mangaID}
		if host := strings.TrimPrefix(strings.ToLower(u.Host), "www."); !slices.Contains(hosts[k], host) {
			hosts[k] = append(hosts[k], host)
		}
	}
	return hosts, nil
}

// Moved updates the library after a directory was renamed from one path to another:
// a manga folder, or a directory with manga folders (e.g. a provider's).
func (l Library) Moved(from, to string) error {
	if err := l.DB.InTx(func(tx *sql.Tx) error { return moved(tx, from, to) }); err != nil {
		return fmt.Errorf("move %s in the library: %w", from, err)
	}
	return nil
}

func moved(tx *sql.Tx, from, to string) error {
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

// MangaFor is the library manga the provider's manga is downloaded into: the one it was
// downloaded into (also under an old provider id), else a manga folder sharing a metadata
// id, matched like FindMangaDir does on download. With providerDirs that folder must be in
// a directory named after the provider (its id or name, current or old).
func (l Library) MangaFor(provider libmangal.ProviderInfo, manga mangadata.Manga, providerDirs bool) (Manga, bool, error) {
	match, err := l.Match(provider, manga.Info().ID, MangaMetadataIDs(manga), providerDirs)
	return match.Manga, match.Linked, err
}

// LibraryMatch is the library manga a provider's manga matches.
type LibraryMatch struct {
	Manga Manga
	// Linked is the manga its downloads go to: the one it was downloaded into, else a
	// folder sharing a metadata id where its downloads would go (see MangaFor).
	Linked bool
	// Elsewhere is a folder sharing a metadata id in another provider's directory (with
	// provider directories): its downloads wouldn't go there, the manga can be moved there.
	Elsewhere bool
}

// Match is what the provider's manga (its id and metadata ids, e.g. its bindings')
// matches in the library, see LibraryMatch.
func (l Library) Match(provider libmangal.ProviderInfo, mangaID string, ids []metadata.ID, providerDirs bool) (LibraryMatch, error) {
	for _, id := range providerIDs(provider) {
		m, ok, err := l.MangaBySource(id, mangaID)
		if err != nil || ok {
			return LibraryMatch{Manga: m, Linked: ok}, err
		}
	}
	if len(ids) == 0 {
		return LibraryMatch{}, nil
	}
	mangas, err := l.mangas(`WHERE m.dir != '' AND m.metadata_ids != '[]' ORDER BY m.id`)
	if err != nil {
		return LibraryMatch{}, fmt.Errorf("find the library manga of %s %q: %w", provider.ID, mangaID, err)
	}
	return matchByIDs(mangas, provider, mangaID, ids, providerDirs), nil
}

// MatchLibrary is Library.Match over mangas already loaded (Library.Mangas), to match
// many provider mangas.
func MatchLibrary(mangas []Manga, provider libmangal.ProviderInfo, mangaID string, ids []metadata.ID, providerDirs bool) LibraryMatch {
	for _, id := range providerIDs(provider) {
		for _, m := range mangas {
			if slices.ContainsFunc(m.Sources, func(src Source) bool { return src.Provider == id && src.MangaID == mangaID }) {
				return LibraryMatch{Manga: m, Linked: true}
			}
		}
	}
	if len(ids) == 0 {
		return LibraryMatch{}
	}
	byID := slices.Clone(mangas)
	slices.SortFunc(byID, func(a, b Manga) int { return cmp.Compare(a.ID, b.ID) })
	return matchByIDs(byID, provider, mangaID, ids, providerDirs)
}

func providerIDs(provider libmangal.ProviderInfo) []string {
	ids := []string{provider.ID}
	for _, alias := range provider.History {
		ids = append(ids, alias.ID)
	}
	return ids
}

// matchByIDs is the first manga folder (mangas by id) sharing a metadata id: linked when
// it's where the provider's downloads go, else elsewhere.
func matchByIDs(mangas []Manga, provider libmangal.ProviderInfo, mangaID string, ids []metadata.ID, providerDirs bool) LibraryMatch {
	want := libmangal.MangaDirInfo{Provider: provider.ID, Manga: mangadata.MangaInfo{ID: mangaID}, MetadataIDs: ids}
	dirs := append(providerIDs(provider), provider.Name)
	for _, alias := range provider.History {
		if alias.Name != "" {
			dirs = append(dirs, alias.Name)
		}
	}
	var elsewhere *Manga
	for _, m := range mangas {
		if m.Dir == "" || libmangal.MatchMangaDir(provider, want, libmangal.MangaDirInfo{MetadataIDs: m.MetadataIDs}) == libmangal.MangaDirNoMatch {
			continue
		}
		if !providerDirs || slices.Contains(dirs, filepath.Base(m.Parent)) {
			return LibraryMatch{Manga: m, Linked: true}
		}
		if elsewhere == nil {
			elsewhere = &m
		}
	}
	if elsewhere != nil {
		return LibraryMatch{Manga: *elsewhere, Elsewhere: true}
	}
	return LibraryMatch{}
}

// MangaMetadataIDs are the manga's metadata ids other than the provider's own, like
// libmangal writes them in the MangaDirInfo.
func MangaMetadataIDs(manga mangadata.Manga) []metadata.ID {
	meta := manga.Metadata()
	if meta == nil {
		return nil
	}
	var ids []metadata.ID
	for _, id := range append([]metadata.ID{meta.ID()}, meta.ExtraIDs()...) {
		if id.Source != metadata.IDSourceProvider && id.Raw != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// SetDownloadPath updates the path of a recorded file (it was moved).
func (l Library) SetDownloadPath(from, to string) error {
	if _, err := l.DB.Exec(`UPDATE downloads SET path = ? WHERE path = ?`, to, from); err != nil {
		return fmt.Errorf("move download %s: %w", from, err)
	}
	return nil
}

// Retitle records the new title of a chapter file, moved from one path to another (the
// same one when its name didn't change): its download's and its library chapter's.
func (l Library) Retitle(from, to, title string) error {
	err := l.DB.InTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE downloads SET path = ?, chapter_title = ? WHERE path = ?`, to, title, from); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE library_chapters SET name = ? WHERE id = (SELECT chapter_id FROM downloads WHERE path = ?)`, title, to)
		return err
	})
	if err != nil {
		return fmt.Errorf("retitle download %s: %w", from, err)
	}
	return nil
}

// RemoveDownload forgets a recorded file, and its chapter if it has no other file.
func (l Library) RemoveDownload(path string) error {
	err := l.DB.InTx(func(tx *sql.Tx) error {
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
	return l.RemoveMangas(id)
}

// RemoveMangas forgets the library mangas, as RemoveManga, all or none.
func (l Library) RemoveMangas(ids ...int64) error {
	return l.DB.InTx(func(tx *sql.Tx) error {
		for _, id := range ids {
			if _, err := tx.Exec(`DELETE FROM downloads WHERE chapter_id IN (SELECT id FROM library_chapters WHERE manga = ?)`, id); err != nil {
				return fmt.Errorf("remove manga %d: %w", id, err)
			}
			if _, err := tx.Exec(`DELETE FROM library_mangas WHERE id = ?`, id); err != nil {
				return fmt.Errorf("remove manga %d: %w", id, err)
			}
			if err := forgetSubject(tx, fmt.Sprintf("manga/%d", id)); err != nil {
				return err
			}
		}
		return nil
	})
}

// MergeMove is a file of the merged manga moved into the kept one.
type MergeMove struct {
	From, To string
}

// MergeDownload records a file of a manga moved into keep (to keep's chapter).
func (l Library) MergeDownload(keep int64, m MergeMove) error {
	if err := l.DB.InTx(func(tx *sql.Tx) error { return mergeDownload(tx, keep, m) }); err != nil {
		return fmt.Errorf("merge download %s: %w", m.From, err)
	}
	return nil
}

func mergeDownload(tx *sql.Tx, keep int64, m MergeMove) error {
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
	_, err = tx.Exec(`UPDATE downloads SET path = ?, chapter_id = ? WHERE path = ?`, m.To, chapter, m.From)
	return err
}

// Merge joins other into keep: the moved files get keep's chapters, the trashed files
// (chapters keep already had) are forgotten, other's sources, reads, watches and metadata ids go to
// keep and other is removed.
func (l Library) Merge(keep, other int64, moved []MergeMove, trashed []string) error {
	err := l.DB.InTx(func(tx *sql.Tx) error {
		for _, path := range trashed {
			if _, err := tx.Exec(`DELETE FROM downloads WHERE path = ?`, path); err != nil {
				return err
			}
		}
		for _, m := range moved {
			if err := mergeDownload(tx, keep, m); err != nil {
				return err
			}
		}
		if err := keepCurrent(tx, keep, other); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE library_sources SET manga = ? WHERE manga = ?`, keep, other); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE reads SET library_manga = ? WHERE library_manga = ?`, keep, other); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE watches SET library_manga = ? WHERE library_manga = ?`, keep, other); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE OR IGNORE link_picks SET manga = ? WHERE manga = ?`, keep, other); err != nil {
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
		if _, err := tx.Exec(`UPDATE library_mangas SET
	hold = CASE WHEN hold = '' THEN (SELECT hold FROM library_mangas WHERE id = ?1) ELSE hold END,
	new_provider = CASE WHEN new_provider = '' THEN (SELECT new_provider FROM library_mangas WHERE id = ?1) ELSE new_provider END
WHERE id = ?2`, other, keep); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM library_mangas WHERE id = ?`, other); err != nil {
			return err
		}
		return forgetSubject(tx, fmt.Sprintf("manga/%d", other))
	})
	if err != nil {
		return fmt.Errorf("merge manga %d into %d: %w", other, keep, err)
	}
	return nil
}

// keepCurrent keeps keep's current source current once other's sources join it: its
// moved time becomes now, so other's later downloads don't make one of its sources the
// current one (the folder stays keep's, as its .libmangal.json).
func keepCurrent(tx *sql.Tx, keep, other int64) error {
	var n int
	if err := tx.QueryRow(`SELECT count(*) FROM library_sources WHERE manga = ?`, other).Scan(&n); err != nil || n == 0 {
		return err
	}
	_, err := tx.Exec(`UPDATE library_sources SET moved = ?1 WHERE (provider, manga_id) = (
	SELECT s.provider, s.manga_id FROM library_sources s WHERE s.manga = ?2
	ORDER BY max(s.moved, coalesce((SELECT max(d.downloaded) FROM downloads d WHERE d.provider = s.provider AND d.manga_id = s.manga_id), 0)) DESC, s.provider
	LIMIT 1)`, unix(time.Now()), keep)
	return err
}

// MangaMove is a library manga moving to another provider's manga (library.MovePlan).
type MangaMove struct {
	Manga int64
	// Source is the provider's manga, the manga's other sources stay.
	Source Source
	// Title is the library title after, empty keeps it.
	Title string
	// From and To are the manga's folder before and after, the same when it stays.
	From, To string
	// Watch is re-linked to the source (its bindings stay), 0 if none.
	Watch int64
	// Bindings are copied to the source, those of a source it has already are skipped.
	Bindings []Binding
	At       time.Time
}

// Move links the library manga to another provider's manga, moves its folder and re-links
// its watch, in one transaction. A source of another library manga is an error.
func (l Library) Move(m MangaMove) error {
	err := l.DB.InTx(func(tx *sql.Tx) error {
		var other int64
		err := tx.QueryRow(`SELECT manga FROM library_sources WHERE provider = ? AND manga_id = ?`, m.Source.Provider, m.Source.MangaID).Scan(&other)
		switch {
		case err == nil && other != m.Manga:
			return fmt.Errorf("it's library manga %d already", other)
		case err != nil && !errors.Is(err, sql.ErrNoRows):
			return err
		}
		if _, err := tx.Exec(`INSERT INTO library_sources (provider, manga_id, manga, title, moved) VALUES (?, ?, ?, ?, ?)
ON CONFLICT (provider, manga_id) DO UPDATE SET title = excluded.title, moved = excluded.moved`, m.Source.Provider, m.Source.MangaID, m.Manga, m.Source.Title, unix(m.At)); err != nil {
			return err
		}
		// on another provider now
		if _, err := tx.Exec(`UPDATE library_mangas SET new_provider = '' WHERE id = ?`, m.Manga); err != nil {
			return err
		}
		if m.Title != "" {
			if _, err := tx.Exec(`UPDATE library_mangas SET title = ? WHERE id = ?`, m.Title, m.Manga); err != nil {
				return err
			}
		}
		if m.From != m.To {
			if err := moved(tx, m.From, m.To); err != nil {
				return err
			}
		}
		if m.Watch != 0 {
			if err := relinkWatch(tx, m.Watch, m.Source.Provider, m.Source.MangaID, m.Source.Title, m.Manga, m.At, false); err != nil {
				return err
			}
		}
		for _, b := range m.Bindings {
			if _, err := tx.Exec(`INSERT INTO metadata_bindings (provider, manga_id, source, metadata_id, updated) VALUES (?, ?, ?, ?, ?)
ON CONFLICT (provider, manga_id, source) DO NOTHING`, m.Source.Provider, m.Source.MangaID, b.Source, b.ID, unix(m.At)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("move library manga %d to %s %q: %w", m.Manga, m.Source.Provider, m.Source.MangaID, err)
	}
	return nil
}

// AddSource links the provider's manga to the library manga when no library manga has it
// and the manga has none of that provider yet; false when it didn't.
func (l Library) AddSource(manga int64, src Source) (bool, error) {
	var added bool
	err := l.DB.InTx(func(tx *sql.Tx) (err error) {
		added, err = addSource(tx, manga, src)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("add source %s %q to library manga %d: %w", src.Provider, src.MangaID, manga, err)
	}
	return added, nil
}

func addSource(tx *sql.Tx, manga int64, src Source) (bool, error) {
	if src.Provider == "" || src.MangaID == "" || strings.HasPrefix(src.MangaID, PlaceholderPrefix) {
		return false, nil
	}
	res, err := tx.Exec(`INSERT INTO library_sources (provider, manga_id, manga, title)
SELECT ?1, ?2, ?3, ?4 WHERE NOT EXISTS (SELECT 1 FROM library_sources WHERE manga = ?3 AND provider = ?1)
ON CONFLICT (provider, manga_id) DO NOTHING`, src.Provider, src.MangaID, manga, src.Title)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n != 0, nil
}

// Backfill is what BackfillSources found.
type Backfill struct {
	FromWatches, FromDownloads int
}

// BackfillSources gives the library mangas without a source (adopted by a scan) the ones
// known elsewhere: their linked watches' mangas, else the provider manga of their recorded
// downloads (the latest downloaded one when they disagree). A source another library
// manga has stays its.
func (l Library) BackfillSources() (Backfill, error) {
	var found Backfill
	err := l.DB.InTx(func(tx *sql.Tx) error {
		mangas, err := queryAll(tx, func(rows *sql.Rows) (int64, error) {
			var id int64
			err := rows.Scan(&id)
			return id, err
		}, `SELECT id FROM library_mangas m WHERE NOT EXISTS (SELECT 1 FROM library_sources s WHERE s.manga = m.id)`)
		if err != nil {
			return err
		}
		scanSource := func(rows *sql.Rows) (Source, error) {
			var src Source
			err := rows.Scan(&src.Provider, &src.MangaID, &src.Title)
			return src, err
		}
		for _, id := range mangas {
			watched, err := queryAll(tx, scanSource, `SELECT provider, manga_id, title FROM watches WHERE library_manga = ? ORDER BY id`, id)
			if err != nil {
				return err
			}
			added := false
			for _, src := range watched {
				ok, err := addSource(tx, id, src)
				if err != nil {
					return err
				}
				if ok {
					found.FromWatches++
					added = true
				}
			}
			if added {
				continue
			}
			downloaded, err := queryAll(tx, scanSource, `SELECT d.provider, d.manga_id, max(d.manga_title) FROM downloads d
JOIN library_chapters c ON c.id = d.chapter_id
WHERE c.manga = ? AND d.provider != '' AND d.manga_id != ''
GROUP BY d.provider, d.manga_id ORDER BY max(d.downloaded) DESC LIMIT 1`, id)
			if err != nil {
				return err
			}
			for _, src := range downloaded {
				ok, err := addSource(tx, id, src)
				if err != nil {
					return err
				}
				if ok {
					found.FromDownloads++
				}
			}
		}
		return nil
	})
	if err != nil {
		return found, fmt.Errorf("find the sources of the library: %w", err)
	}
	return found, nil
}

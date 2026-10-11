package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// History is the chapters read, in the data database.
//
// A chapter is one row per provider, manga and chapter number (like the library's
// chapters): opening it again counts it and moves its last read.
type History struct {
	DB *DB
}

// Read is a chapter read.
type Read struct {
	Provider   string
	MangaID    string
	MangaTitle string

	Volume       float32
	Chapter      float32
	ChapterTitle string
	ChapterURL   string

	// LibraryManga is the library manga the provider's manga is downloaded into, 0 if
	// none (when read).
	LibraryManga int64

	// Count is how many times it was opened.
	Count int
	// First and Last are when it was first and last opened.
	First, Last time.Time
}

const historyTable = `CREATE TABLE reads (
	provider      TEXT NOT NULL,
	manga_id      TEXT NOT NULL,
	manga_title   TEXT NOT NULL,
	volume        REAL NOT NULL,
	chapter       REAL NOT NULL,
	chapter_title TEXT NOT NULL,
	chapter_url   TEXT NOT NULL,
	library_manga INTEGER REFERENCES library_mangas (id) ON DELETE SET NULL,
	count         INTEGER NOT NULL,
	first_read    INTEGER NOT NULL,
	last_read     INTEGER NOT NULL,
	PRIMARY KEY (provider, manga_id, chapter)
);
CREATE INDEX reads_last ON reads (last_read);
CREATE INDEX reads_library_manga ON reads (library_manga)`

// Record records a read of the chapter at r.Last (Count, First are ignored).
func (h History) Record(r Read) error {
	var libraryManga sql.NullInt64
	if r.LibraryManga != 0 {
		libraryManga = sql.NullInt64{Int64: r.LibraryManga, Valid: true}
	}
	_, err := h.DB.Exec(`INSERT INTO reads (provider, manga_id, manga_title, volume, chapter, chapter_title, chapter_url, library_manga, count, first_read, last_read)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
ON CONFLICT (provider, manga_id, chapter) DO UPDATE SET manga_title = excluded.manga_title, volume = excluded.volume,
	chapter_title = excluded.chapter_title, chapter_url = excluded.chapter_url,
	library_manga = coalesce(excluded.library_manga, library_manga), count = count + 1, last_read = excluded.last_read`,
		r.Provider, r.MangaID, r.MangaTitle, r.Volume, r.Chapter, r.ChapterTitle, r.ChapterURL, libraryManga, r.Last.Unix(), r.Last.Unix())
	if err != nil {
		return fmt.Errorf("record read of %s %s chapter %g: %w", r.Provider, r.MangaID, r.Chapter, err)
	}
	return nil
}

// List lists the reads of a provider and manga, empty for any, last read first.
func (h History) List(provider, mangaID string) ([]Read, error) {
	reads, err := queryAll(h.DB, scanRead, `SELECT provider, manga_id, manga_title, volume, chapter, chapter_title, chapter_url,
	coalesce(library_manga, 0), count, first_read, last_read
FROM reads WHERE (?1 = '' OR provider = ?1) AND (?2 = '' OR manga_id = ?2)
ORDER BY last_read DESC, provider, manga_id, chapter DESC`, provider, mangaID)
	if err != nil {
		return nil, fmt.Errorf("list reads: %w", err)
	}
	return reads, nil
}

func scanRead(rows *sql.Rows) (Read, error) {
	var r Read
	var first, last int64
	err := rows.Scan(&r.Provider, &r.MangaID, &r.MangaTitle, &r.Volume, &r.Chapter, &r.ChapterTitle, &r.ChapterURL,
		&r.LibraryManga, &r.Count, &first, &last)
	r.First, r.Last = time.Unix(first, 0), time.Unix(last, 0)
	return r, err
}

// ReadChapters are the chapter numbers read of the manga under any of the provider ids
// (current and old), or of the library manga (from any provider) if not 0.
func (h History) ReadChapters(providers []string, mangaID string, libraryManga int64) (map[float32]bool, error) {
	read := map[float32]bool{}
	for _, provider := range providers {
		numbers, err := queryAll(h.DB, scanNumber, `SELECT chapter FROM reads WHERE provider = ? AND manga_id = ?`, provider, mangaID)
		if err != nil {
			return nil, fmt.Errorf("read chapters of %s %s: %w", provider, mangaID, err)
		}
		for _, n := range numbers {
			read[n] = true
		}
	}
	if libraryManga != 0 {
		numbers, err := queryAll(h.DB, scanNumber, `SELECT chapter FROM reads WHERE library_manga = ?`, libraryManga)
		if err != nil {
			return nil, fmt.Errorf("read chapters of library manga %d: %w", libraryManga, err)
		}
		for _, n := range numbers {
			read[n] = true
		}
	}
	return read, nil
}

func scanNumber(rows *sql.Rows) (float32, error) {
	var n float32
	err := rows.Scan(&n)
	return n, err
}

// Clear forgets the reads of a provider and manga, empty for any. It returns how many
// chapters were forgotten.
func (h History) Clear(provider, mangaID string) (int64, error) {
	res, err := h.DB.Exec(`DELETE FROM reads WHERE (?1 = '' OR provider = ?1) AND (?2 = '' OR manga_id = ?2)`, provider, mangaID)
	if err != nil {
		return 0, fmt.Errorf("clear history: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// migrateReads moves the reads of an old provider id to the new one, a chapter read under
// both is merged.
func migrateReads(tx *sql.Tx, oldID, newID string) error {
	if _, err := tx.Exec(`UPDATE reads AS n SET count = n.count + o.count, first_read = min(n.first_read, o.first_read),
	last_read = max(n.last_read, o.last_read), library_manga = coalesce(n.library_manga, o.library_manga)
FROM reads AS o WHERE n.provider = ?2 AND o.provider = ?1 AND o.manga_id = n.manga_id AND o.chapter = n.chapter`, oldID, newID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM reads WHERE provider = ?1 AND EXISTS (SELECT 1 FROM reads n WHERE n.provider = ?2 AND n.manga_id = reads.manga_id AND n.chapter = reads.chapter)`, oldID, newID); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE reads SET provider = ? WHERE provider = ?`, newID, oldID)
	return err
}

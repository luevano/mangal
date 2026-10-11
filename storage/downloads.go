package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// Download is a downloaded chapter file.
type Download struct {
	// Path of the chapter file (or directory, for the images format).
	Path string
	// ChapterID is the library chapter, set when recorded.
	ChapterID int64

	Provider   string
	MangaID    string
	MangaTitle string

	Volume       float32
	Chapter      float32
	ChapterTitle string
	ChapterURL   string

	Format string
	// Pages is 0 if unknown (e.g. it was already there when first recorded).
	Pages      int
	Downloaded time.Time
}

// Downloads are the downloaded chapters.
type Downloads struct {
	DB *DB
}

func recordDownload(tx *sql.Tx, dl Download) error {
	_, err := tx.Exec(`INSERT INTO downloads (path, chapter_id, provider, manga_id, manga_title, volume, chapter, chapter_title, chapter_url, format, pages, downloaded)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (path) DO UPDATE SET chapter_id = excluded.chapter_id, provider = excluded.provider, manga_id = excluded.manga_id,
	manga_title = excluded.manga_title, volume = excluded.volume, chapter = excluded.chapter, chapter_title = excluded.chapter_title,
	chapter_url = excluded.chapter_url, format = excluded.format,
	pages = CASE excluded.pages WHEN 0 THEN pages ELSE excluded.pages END, downloaded = excluded.downloaded`,
		dl.Path, dl.ChapterID, dl.Provider, dl.MangaID, dl.MangaTitle, dl.Volume, dl.Chapter, dl.ChapterTitle, dl.ChapterURL,
		dl.Format, dl.Pages, dl.Downloaded.Unix())
	return err
}

// List lists the downloads of a provider and manga, empty for any, ordered by manga and chapter.
func (d Downloads) List(provider, mangaID string) ([]Download, error) {
	return queryDownloads(d.DB, `WHERE (? = '' OR d.provider = ?) AND (? = '' OR d.manga_id = ?)`, provider, provider, mangaID, mangaID)
}

func queryDownloads(db *DB, where string, args ...any) ([]Download, error) {
	rows, err := db.Query(`SELECT d.path, coalesce(d.chapter_id, 0), d.provider, d.manga_id, d.manga_title, d.volume, d.chapter,
	d.chapter_title, d.chapter_url, d.format, d.pages, d.downloaded
FROM downloads d LEFT JOIN library_chapters c ON c.id = d.chapter_id `+where+`
ORDER BY d.manga_title, d.provider, d.manga_id, d.volume, d.chapter, d.format`, args...)
	if err != nil {
		return nil, fmt.Errorf("list downloads: %w", err)
	}
	defer rows.Close()

	var downloads []Download
	for rows.Next() {
		var dl Download
		var downloaded int64
		if err := rows.Scan(&dl.Path, &dl.ChapterID, &dl.Provider, &dl.MangaID, &dl.MangaTitle, &dl.Volume, &dl.Chapter,
			&dl.ChapterTitle, &dl.ChapterURL, &dl.Format, &dl.Pages, &downloaded); err != nil {
			return nil, err
		}
		dl.Downloaded = time.Unix(downloaded, 0)
		downloads = append(downloads, dl)
	}
	return downloads, rows.Err()
}

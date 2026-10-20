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

	// TitleProvider and TitleMangaID are the provider's manga ChapterTitle is from when it
	// isn't Provider's own (a fill, a retitle), TitleChanged when it got it.
	TitleProvider string
	TitleMangaID  string
	TitleChanged  time.Time
}

// downloadTitles (migration 24): where a download's title is from, when not its provider.
const downloadTitles = `ALTER TABLE downloads ADD COLUMN title_provider TEXT NOT NULL DEFAULT '';
ALTER TABLE downloads ADD COLUMN title_manga_id TEXT NOT NULL DEFAULT '';
ALTER TABLE downloads ADD COLUMN title_changed INTEGER NOT NULL DEFAULT 0`

func recordDownload(tx *sql.Tx, dl Download) error {
	_, err := tx.Exec(`INSERT INTO downloads (path, chapter_id, provider, manga_id, manga_title, volume, chapter, chapter_title, chapter_url, format, pages, downloaded,
	title_provider, title_manga_id, title_changed)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (path) DO UPDATE SET chapter_id = excluded.chapter_id, provider = excluded.provider, manga_id = excluded.manga_id,
	manga_title = excluded.manga_title, volume = excluded.volume, chapter = excluded.chapter, chapter_title = excluded.chapter_title,
	chapter_url = excluded.chapter_url, format = excluded.format,
	pages = CASE excluded.pages WHEN 0 THEN pages ELSE excluded.pages END, downloaded = excluded.downloaded,
	title_provider = excluded.title_provider, title_manga_id = excluded.title_manga_id, title_changed = excluded.title_changed`,
		dl.Path, dl.ChapterID, dl.Provider, dl.MangaID, dl.MangaTitle, dl.Volume, dl.Chapter, dl.ChapterTitle, dl.ChapterURL,
		dl.Format, dl.Pages, dl.Downloaded.Unix(), dl.TitleProvider, dl.TitleMangaID, unix(dl.TitleChanged))
	return err
}

// ListDownloads lists the downloads of a provider and manga, empty for any, ordered by manga
// and chapter.
func (l Library) ListDownloads(provider, mangaID string) ([]Download, error) {
	return queryDownloads(l.DB, `WHERE (?1 = '' OR d.provider = ?1) AND (?2 = '' OR d.manga_id = ?2)
ORDER BY d.manga_title, d.provider, d.manga_id, d.volume, d.chapter, d.format`, provider, mangaID)
}

func queryDownloads(db *DB, where string, args ...any) ([]Download, error) {
	downloads, err := queryAll(db, func(rows *sql.Rows) (Download, error) {
		var dl Download
		var downloaded, titleChanged int64
		err := rows.Scan(&dl.Path, &dl.ChapterID, &dl.Provider, &dl.MangaID, &dl.MangaTitle, &dl.Volume, &dl.Chapter,
			&dl.ChapterTitle, &dl.ChapterURL, &dl.Format, &dl.Pages, &downloaded, &dl.TitleProvider, &dl.TitleMangaID, &titleChanged)
		dl.Downloaded, dl.TitleChanged = time.Unix(downloaded, 0), fromUnix(titleChanged)
		return dl, err
	}, `SELECT d.path, coalesce(d.chapter_id, 0), d.provider, d.manga_id, d.manga_title, d.volume, d.chapter,
	d.chapter_title, d.chapter_url, d.format, d.pages, d.downloaded, d.title_provider, d.title_manga_id, d.title_changed
FROM downloads d LEFT JOIN library_chapters c ON c.id = d.chapter_id `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("list downloads: %w", err)
	}
	return downloads, nil
}

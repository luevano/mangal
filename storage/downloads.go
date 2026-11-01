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

	// PageFormat is what its pages are by their names: webp, jpeg, png..., mixed, empty for
	// unknown (a PDF). PageFormatMTime is the file's modification time it was read at, a
	// scan only reads it again when that changes.
	PageFormat      string
	PageFormatMTime int64
}

// downloadTitles (migration 24): where a download's title is from, when not its provider.
const downloadTitles = `ALTER TABLE downloads ADD COLUMN title_provider TEXT NOT NULL DEFAULT '';
ALTER TABLE downloads ADD COLUMN title_manga_id TEXT NOT NULL DEFAULT '';
ALTER TABLE downloads ADD COLUMN title_changed INTEGER NOT NULL DEFAULT 0`

// downloadPageFormat (migration 30): what a chapter's pages are underneath.
const downloadPageFormat = `ALTER TABLE downloads ADD COLUMN page_format TEXT NOT NULL DEFAULT '';
ALTER TABLE downloads ADD COLUMN page_format_mtime INTEGER NOT NULL DEFAULT 0`

func recordDownload(tx *sql.Tx, dl Download) error {
	_, err := tx.Exec(`INSERT INTO downloads (path, chapter_id, provider, manga_id, manga_title, volume, chapter, chapter_title, chapter_url, format, pages, downloaded,
	title_provider, title_manga_id, title_changed, page_format, page_format_mtime)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (path) DO UPDATE SET chapter_id = excluded.chapter_id, provider = excluded.provider, manga_id = excluded.manga_id,
	manga_title = excluded.manga_title, volume = excluded.volume, chapter = excluded.chapter, chapter_title = excluded.chapter_title,
	chapter_url = excluded.chapter_url, format = excluded.format,
	pages = CASE excluded.pages WHEN 0 THEN pages ELSE excluded.pages END, downloaded = excluded.downloaded,
	title_provider = excluded.title_provider, title_manga_id = excluded.title_manga_id, title_changed = excluded.title_changed,
	page_format = excluded.page_format, page_format_mtime = excluded.page_format_mtime`,
		dl.Path, dl.ChapterID, dl.Provider, dl.MangaID, dl.MangaTitle, dl.Volume, dl.Chapter, dl.ChapterTitle, dl.ChapterURL,
		dl.Format, dl.Pages, dl.Downloaded.Unix(), dl.TitleProvider, dl.TitleMangaID, unix(dl.TitleChanged), dl.PageFormat, dl.PageFormatMTime)
	return err
}

// SetPageFormat records what the pages of a chapter file are, read at its modification time.
func (l Library) SetPageFormat(path, format string, mtime int64) error {
	if _, err := l.DB.Exec(`UPDATE downloads SET page_format = ?, page_format_mtime = ? WHERE path = ?`, format, mtime, path); err != nil {
		return fmt.Errorf("page format of %s: %w", path, err)
	}
	return nil
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
			&dl.ChapterTitle, &dl.ChapterURL, &dl.Format, &dl.Pages, &downloaded, &dl.TitleProvider, &dl.TitleMangaID, &titleChanged, &dl.PageFormat, &dl.PageFormatMTime)
		dl.Downloaded, dl.TitleChanged = time.Unix(downloaded, 0), fromUnix(titleChanged)
		return dl, err
	}, `SELECT d.path, coalesce(d.chapter_id, 0), d.provider, d.manga_id, d.manga_title, d.volume, d.chapter,
	d.chapter_title, d.chapter_url, d.format, d.pages, d.downloaded, d.title_provider, d.title_manga_id, d.title_changed, d.page_format, d.page_format_mtime
FROM downloads d LEFT JOIN library_chapters c ON c.id = d.chapter_id `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("list downloads: %w", err)
	}
	return downloads, nil
}

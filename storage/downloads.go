package storage

import (
	"fmt"
	"time"
)

// Download is a downloaded chapter.
type Download struct {
	// Path of the chapter file (or directory, for the images format).
	Path string

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

// Record adds or updates a downloaded chapter, keeping the known page count if the new one is 0.
func (d Downloads) Record(dl Download) error {
	_, err := d.DB.Exec(`INSERT INTO downloads (path, provider, manga_id, manga_title, volume, chapter, chapter_title, chapter_url, format, pages, downloaded)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (path) DO UPDATE SET provider = excluded.provider, manga_id = excluded.manga_id, manga_title = excluded.manga_title,
	volume = excluded.volume, chapter = excluded.chapter, chapter_title = excluded.chapter_title, chapter_url = excluded.chapter_url,
	format = excluded.format, pages = CASE excluded.pages WHEN 0 THEN pages ELSE excluded.pages END, downloaded = excluded.downloaded`,
		dl.Path, dl.Provider, dl.MangaID, dl.MangaTitle, dl.Volume, dl.Chapter, dl.ChapterTitle, dl.ChapterURL,
		dl.Format, dl.Pages, dl.Downloaded.Unix())
	if err != nil {
		return fmt.Errorf("record download %s: %w", dl.Path, err)
	}
	return nil
}

// List lists the downloads of a provider and manga, empty for any, ordered by manga and chapter.
func (d Downloads) List(provider, mangaID string) ([]Download, error) {
	rows, err := d.DB.Query(`SELECT path, provider, manga_id, manga_title, volume, chapter, chapter_title, chapter_url, format, pages, downloaded
FROM downloads WHERE (? = '' OR provider = ?) AND (? = '' OR manga_id = ?)
ORDER BY manga_title, provider, manga_id, volume, chapter, format`, provider, provider, mangaID, mangaID)
	if err != nil {
		return nil, fmt.Errorf("list downloads: %w", err)
	}
	defer rows.Close()

	var downloads []Download
	for rows.Next() {
		var dl Download
		var downloaded int64
		if err := rows.Scan(&dl.Path, &dl.Provider, &dl.MangaID, &dl.MangaTitle, &dl.Volume, &dl.Chapter,
			&dl.ChapterTitle, &dl.ChapterURL, &dl.Format, &dl.Pages, &downloaded); err != nil {
			return nil, err
		}
		dl.Downloaded = time.Unix(downloaded, 0)
		downloads = append(downloads, dl)
	}
	return downloads, rows.Err()
}

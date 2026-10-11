package client

import (
	"context"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
)

// DownloadChapter downloads the chapter and records it in the downloads, unless it went
// to the temp dir (read without download_on_read).
func DownloadChapter(ctx context.Context, client *libmangal.Client, chapter mangadata.Chapter, options libmangal.DownloadOptions) (*metadata.DownloadedChapter, error) {
	down, err := client.DownloadChapterContext(ctx, chapter, options)
	if err != nil || options.Directory == path.TempDir() {
		return down, err
	}

	// the chapter is downloaded either way, a failed record is only logged
	if err := recordDownload(client, chapter, options.Format, down); err != nil {
		log.Log("%s", err)
	}
	return down, nil
}

func recordDownload(client *libmangal.Client, chapter mangadata.Chapter, format libmangal.Format, down *metadata.DownloadedChapter) error {
	_, dataDB, err := cache.DBs()
	if err != nil {
		return err
	}
	manga := chapter.Volume().Manga().Info()
	info := chapter.Info()
	return storage.Downloads{DB: dataDB}.Record(storage.Download{
		Path:         down.Path(),
		Provider:     client.Info().ID,
		MangaID:      manga.ID,
		MangaTitle:   manga.Title,
		Volume:       chapter.Volume().Info().Number,
		Chapter:      info.Number,
		ChapterTitle: info.Title,
		ChapterURL:   info.URL,
		Format:       format.String(),
		Pages:        down.Pages,
		Downloaded:   time.Now(),
	})
}

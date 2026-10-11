package client

import (
	"context"
	"path/filepath"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
)

// DownloadChapter downloads the chapter and records it in the library, unless it went to
// the temp dir (read without download_on_read). A chapter that's already there (maybe
// from another provider, see DownloadedChapter.Skipped) isn't downloaded again.
func DownloadChapter(ctx context.Context, client *libmangal.Client, chapter mangadata.Chapter, options libmangal.DownloadOptions) (*metadata.DownloadedChapter, error) {
	down, err := client.DownloadChapterContext(ctx, chapter, options)
	if err != nil || options.Directory == path.TempDir() {
		return down, err
	}
	if down.Skipped != "" {
		log.Log("chapter %q skipped: %s", chapter, down.Skipped)
	}

	// the chapter is downloaded either way, a failed record is only logged
	if err := recordDownload(client, chapter, options, down); err != nil {
		log.Log("%s", err)
	}
	return down, nil
}

func recordDownload(client *libmangal.Client, chapter mangadata.Chapter, options libmangal.DownloadOptions, down *metadata.DownloadedChapter) error {
	_, dataDB, err := cache.DBs()
	if err != nil {
		return err
	}
	library := storage.Library{DB: dataDB}
	// a skipped chapter keeps who downloaded it
	if down.Skipped != "" {
		if recorded, err := library.HasDownload(down.Path()); err != nil || recorded {
			return err
		}
	}

	folder := down.Directory
	if options.CreateVolumeDir {
		folder = filepath.Dir(folder)
	}
	parent, dir := folder, ""
	if options.CreateMangaDir {
		parent, dir = filepath.Dir(folder), filepath.Base(folder)
	}

	manga := chapter.Volume().Manga().Info()
	info := chapter.Info()
	return library.RecordDownload(parent, dir, storage.Download{
		Path:         down.Path(),
		Provider:     client.Info().ID,
		MangaID:      manga.ID,
		MangaTitle:   manga.Title,
		Volume:       chapter.Volume().Info().Number,
		Chapter:      info.Number,
		ChapterTitle: info.Title,
		ChapterURL:   info.URL,
		Format:       options.Format.String(),
		Pages:        down.Pages,
		Downloaded:   time.Now(),
	})
}

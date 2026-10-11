package chapters

import (
	"path/filepath"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

// libraryDownloads are the chapters of the manga's library folder by number and format
// (their paths): the library is the one answer to "is it downloaded", whichever provider
// got it. With a scanner, the folder is checked against the files first.
func libraryDownloads(lib storage.Library, scanner *library.Scanner, root string, m storage.Manga, providerDirs bool) (map[float32]map[libmangal.Format]string, error) {
	if scanner != nil {
		if _, err := scanner.ScanManga(m, root, providerDirs); err != nil {
			return nil, err
		}
	}
	downloads, err := lib.Downloads(m.ID)
	if err != nil {
		return nil, err
	}
	paths := map[float32]map[libmangal.Format]string{}
	for _, dl := range downloads {
		format, err := libmangal.FormatString(dl.Format)
		if err != nil {
			continue
		}
		if paths[dl.Chapter] == nil {
			paths[dl.Chapter] = map[libmangal.Format]string{}
		}
		paths[dl.Chapter][format] = dl.Path
	}
	return paths, nil
}

// readChapters are the chapter numbers read of the manga (under the provider's current or
// old ids), or of its library manga (libraryManga, 0 if none) from any provider.
func readChapters(history storage.History, provider libmangal.ProviderInfo, manga mangadata.Manga, libraryManga int64) (map[float32]bool, error) {
	ids := []string{provider.ID}
	for _, alias := range provider.History {
		ids = append(ids, alias.ID)
	}
	return history.ReadChapters(ids, manga.Info().ID, libraryManga)
}

// tempFiles are the chapters read without download_on_read, by format, from the temp dir.
func tempFiles(fsys afero.Fs, c *libmangal.Client, temp string, chapters []mangadata.Chapter) []map[libmangal.Format]string {
	files := make([]map[libmangal.Format]string, len(chapters))
	for i, ch := range chapters {
		dir := filepath.Join(temp, c.ProviderName(c.Info()), c.MangaName(ch.Volume().Manga()), c.VolumeName(ch.Volume()))
		for _, format := range libmangal.FormatValues() {
			path := filepath.Join(dir, c.ChapterName(ch, format))
			if exists, err := afero.Exists(fsys, path); err == nil && exists {
				if files[i] == nil {
					files[i] = map[libmangal.Format]string{}
				}
				files[i][format] = path
			}
		}
	}
	return files
}

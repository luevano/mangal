package chapters

import (
	"path/filepath"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

// downloadPaths are the library manga's chapters by number and format (their paths):
// the library is the one answer to "is it downloaded", whichever provider got it.
func downloadPaths(downloads []storage.Download) map[float32]map[libmangal.Format]string {
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
	return paths
}

// readSet are the chapter numbers read.
func readSet(read []float32) map[float32]bool {
	set := make(map[float32]bool, len(read))
	for _, n := range read {
		set[n] = true
	}
	return set
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

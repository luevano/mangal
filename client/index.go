package client

import (
	"path/filepath"
	"strings"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/storage"
)

// libraryIndex is the library as libmangal's indexes, except for the temp directory: chapters
// read without downloading them go there, they aren't part of the library.
type libraryIndex struct {
	library storage.Library
	temp    string
}

func (i libraryIndex) inTemp(dir string) bool {
	rel, err := filepath.Rel(i.temp, dir)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (i libraryIndex) FindMangaDir(parent string, provider libmangal.ProviderInfo, info libmangal.MangaDirInfo) (string, bool, error) {
	if i.inTemp(parent) {
		return "", false, nil
	}
	return i.library.FindMangaDir(parent, provider, info)
}

func (i libraryIndex) RememberMangaDir(parent, name string, info libmangal.MangaDirInfo) error {
	if i.inTemp(parent) {
		return nil
	}
	return i.library.RememberMangaDir(parent, name, info)
}

func (i libraryIndex) FindChapter(mangaDir string, chapter mangadata.Chapter, format libmangal.Format) (string, string, bool, error) {
	if i.inTemp(mangaDir) {
		return "", "", false, nil
	}
	return i.library.FindChapter(mangaDir, chapter, format)
}

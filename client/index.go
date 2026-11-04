package client

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/library"
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

// MangaDirInfoContext implements libmangal.MangaDirInfoIndex: a download's .libmangal.json
// gets every metadata id the library knows of the manga (library.LinkInfo.MarkerIDs: the
// bindings, the folder's library manga's ids) and the ones its provider reported.
func (i libraryIndex) MangaDirInfoContext(_ context.Context, parent, name string, info libmangal.MangaDirInfo) (libmangal.MangaDirInfo, error) {
	if i.inTemp(parent) {
		return info, nil
	}
	info.AddMetadataIDs(reportedIDs(info.Provider, info.Manga.ID)...)
	m, _, err := i.library.MangaByFolder(parent, name)
	if err != nil {
		return info, err
	}
	bindings := storage.Bindings{DB: i.library.DB}
	// the downloaded provider manga's first, it may not be a source yet
	bound, err := bindings.Get(info.Provider, info.Manga.ID)
	if err != nil {
		return info, err
	}
	for _, src := range m.Sources {
		more, err := bindings.Get(src.Provider, src.MangaID)
		if err != nil {
			return info, err
		}
		bound = append(bound, more...)
	}
	info.MetadataIDs = library.LinkInfoOf(m, bound, &info).MarkerIDs()
	return info, nil
}

func (i libraryIndex) FindChapter(mangaDir string, chapter mangadata.Chapter, format libmangal.Format) (string, string, bool, error) {
	if i.inTemp(mangaDir) {
		return "", "", false, nil
	}
	return i.library.FindChapter(mangaDir, chapter, format)
}

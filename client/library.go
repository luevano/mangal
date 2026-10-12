package client

import (
	"context"
	"slices"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/client/selector"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/template"
	"github.com/luevano/mangal/v5/util/afs"
	"github.com/luevano/mangal/v5/util/cache"
)

// LibraryFiles is the library over the download directory, with the configured modes.
func LibraryFiles() (library.Files, error) {
	lib, err := cache.Library()
	if err != nil {
		return library.Files{}, err
	}
	return library.Files{
		FS:      afs.Afero,
		Library: lib,
		Root:    path.Downloads(),
		Modes:   LibraryModes(),
	}, nil
}

// LibraryModes are download.mode_file and download.mode_dir.
func LibraryModes() library.Modes {
	return library.Modes{File: config.Download.ModeFile.Get(), Dir: config.Download.ModeDir.Get()}
}

// LibraryMover moves library mangas to other providers, with the configured name
// templates and provider directories.
func LibraryMover() (library.Mover, error) {
	files, err := LibraryFiles()
	if err != nil {
		return library.Mover{}, err
	}
	return library.Mover{
		Files:        files,
		ProviderDirs: config.Download.Provider.CreateDir.Get(),
		ProviderName: template.Provider,
		MangaName:    template.Manga,
		Metadata:     anilist.MetadataByIDs,
	}, nil
}

// LibraryDuplicates are the library mangas sharing a metadata id (library.Duplicates),
// watched ones by the watches of the data database.
func LibraryDuplicates(lib storage.Library) ([]library.Duplicate, error) {
	mangas, err := lib.Mangas()
	if err != nil {
		return nil, err
	}
	bindings, err := storage.Bindings{DB: lib.DB}.List("", "")
	if err != nil {
		return nil, err
	}
	watches, err := storage.Watches{DB: lib.DB}.List()
	if err != nil {
		return nil, err
	}
	return library.Duplicates(mangas, bindings, func(m storage.Manga) bool {
		return slices.ContainsFunc(watches, func(w storage.Watch) bool {
			return w.LibraryManga == m.ID || slices.ContainsFunc(m.Sources, func(src storage.Source) bool {
				return src.Provider == w.Provider && src.MangaID == w.MangaID
			})
		})
	}), nil
}

// Searcher is the client as a selector.Searcher.
func Searcher(c *libmangal.Client) selector.Searcher {
	return searcher{c}
}

type searcher struct{ *libmangal.Client }

func (s searcher) MangaByID(ctx context.Context, id string) (mangadata.Manga, bool, error) {
	return s.MangaByIDContext(ctx, id)
}

func (s searcher) SearchMangas(ctx context.Context, query string) ([]mangadata.Manga, error) {
	return s.SearchMangasContext(ctx, query)
}

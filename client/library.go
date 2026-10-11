package client

import (
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/path"
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

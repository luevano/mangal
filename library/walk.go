package library

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/luevano/libmangal"
	"github.com/spf13/afero"
)

// mangaFolder is a folder under the download directory that may be a manga's.
type mangaFolder struct {
	path string
	// providerDir is the provider directory it's in, empty if it's directly in root.
	providerDir string
	info        libmangal.MangaDirInfo
	// marked folders have a (valid) .libmangal.json.
	marked bool
	// markerErr is why its .libmangal.json couldn't be read (not that there's none)
	markerErr error
}

// walkMangaDirs lists the folders that may be mangas, the one rule for scan, rename and
// migrate: root's folders, and with providerDirs the folders of root's folders (the
// provider directories, root's folders that aren't marked), where root's own folders only
// count if marked. Dot folders (the trash among them) are skipped.
func walkMangaDirs(fsys afero.Fs, root string, providerDirs bool) ([]mangaFolder, error) {
	var folders []mangaFolder
	add := func(parent, providerDir string, unmarked bool) ([]fs.FileInfo, error) {
		entries, err := afero.ReadDir(fsys, parent)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		var dirs []fs.FileInfo
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			path := filepath.Join(parent, e.Name())
			// an unreadable or invalid .libmangal.json is like none
			info, err := libmangal.ReadMangaDirInfo(fsys, path)
			if err == nil {
				folders = append(folders, mangaFolder{path: path, providerDir: providerDir, info: info, marked: true})
				continue
			}
			var markerErr error
			if !errors.Is(err, fs.ErrNotExist) {
				markerErr = err
			}
			dirs = append(dirs, e)
			if unmarked {
				folders = append(folders, mangaFolder{path: path, providerDir: providerDir, markerErr: markerErr})
			}
		}
		return dirs, nil
	}

	dirs, err := add(root, "", !providerDirs)
	if err != nil || !providerDirs {
		return folders, err
	}
	for _, e := range dirs {
		dir := filepath.Join(root, e.Name())
		if _, err := add(dir, dir, true); err != nil {
			return nil, err
		}
	}
	return folders, nil
}

// markedDirs are the folders of walkMangaDirs with a .libmangal.json.
func markedDirs(fsys afero.Fs, root string, providerDirs bool) ([]mangaFolder, error) {
	folders, err := walkMangaDirs(fsys, root, providerDirs)
	var marked []mangaFolder
	for _, f := range folders {
		if f.marked {
			marked = append(marked, f)
		}
	}
	return marked, err
}

// Unmarked are the folders of walkMangaDirs without a .libmangal.json: rename doesn't know
// their manga.
func Unmarked(fsys afero.Fs, root string, providerDirs bool) ([]string, error) {
	folders, err := walkMangaDirs(fsys, root, providerDirs)
	var unmarked []string
	for _, f := range folders {
		if !f.marked {
			unmarked = append(unmarked, f.path)
		}
	}
	return unmarked, err
}

// Folder is a folder under the download directory that may be a manga's: the folders
// scan, rename and mark look at.
type Folder struct {
	Path string
	// ProviderDir is the provider directory it's in, empty if it's directly in the root.
	ProviderDir string
	// Marker is its .libmangal.json, nil if it has none or it's broken (MarkerErr).
	Marker    *libmangal.MangaDirInfo
	MarkerErr error
}

// Folders are the folders under root that may be mangas (root's folders, and with
// providerDirs the folders of the provider directories), with their .libmangal.json.
func Folders(fsys afero.Fs, root string, providerDirs bool) ([]Folder, error) {
	folders, err := walkMangaDirs(fsys, root, providerDirs)
	out := make([]Folder, len(folders))
	for i, f := range folders {
		out[i] = Folder{Path: f.path, ProviderDir: f.providerDir, MarkerErr: f.markerErr}
		if f.marked {
			info := f.info
			out[i].Marker = &info
		}
	}
	return out, err
}

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
			dirs = append(dirs, e)
			if unmarked {
				folders = append(folders, mangaFolder{path: path, providerDir: providerDir})
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

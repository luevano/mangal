package library

import (
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"

	"github.com/luevano/libmangal"
	"github.com/spf13/afero"
)

// OldMarker is a manga directory whose libmangal.MangaDirInfo has an old provider id.
type OldMarker struct {
	Dir      string
	Old, New string
}

// OldMarkers lists the manga directories under dir (and its provider directories when
// providerDirs is set) whose info has an old id of one of the loaders.
func OldMarkers(fsys afero.Fs, dir string, providerDirs bool, loaders []libmangal.ProviderLoader) ([]OldMarker, error) {
	parents := []string{dir}
	if providerDirs {
		entries, err := afero.ReadDir(fsys, dir)
		if err != nil {
			return nil, err
		}
		parents = nil
		for _, entry := range entries {
			if entry.IsDir() {
				parents = append(parents, filepath.Join(dir, entry.Name()))
			}
		}
	}

	var markers []OldMarker
	for _, parent := range parents {
		entries, err := afero.ReadDir(fsys, parent)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			mangaDir := filepath.Join(parent, entry.Name())
			info, err := libmangal.ReadMangaDirInfo(fsys, mangaDir)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if loader, alias, ok := libmangal.FindLoader(loaders, info.Provider); ok && alias != nil {
				markers = append(markers, OldMarker{Dir: mangaDir, Old: info.Provider, New: loader.Info().ID})
			}
		}
	}
	return markers, nil
}

// UpdateMarker writes the new provider id into the directory's info.
func UpdateMarker(fsys afero.Fs, marker OldMarker) error {
	info, err := libmangal.ReadMangaDirInfo(fsys, marker.Dir)
	if err != nil {
		return err
	}
	info.Provider = marker.New
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	return afero.WriteFile(fsys, filepath.Join(marker.Dir, libmangal.FilenameMangaDirInfo), data, 0o644)
}

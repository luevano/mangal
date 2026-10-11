package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/storage"
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

// Migration is the data still under old provider ids, see PlanMigration.
type Migration struct {
	OldIDs  []storage.OldID
	Markers []OldMarker
}

// PlanMigration finds the data under old provider ids: library entries, downloads and
// cache in the databases, and the .libmangal.json files under root.
func PlanMigration(fsys afero.Fs, cacheDB, dataDB *storage.DB, root string, providerDirs bool, loaders []libmangal.ProviderLoader) (Migration, error) {
	oldIDs, err := storage.FindOldIDs(cacheDB, dataDB, loaders)
	if err != nil {
		return Migration{}, err
	}
	markers, err := OldMarkers(fsys, root, providerDirs, loaders)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Migration{}, err
	}
	return Migration{OldIDs: oldIDs, Markers: markers}, nil
}

// Empty is true when there's nothing to migrate.
func (m Migration) Empty() bool {
	return len(m.OldIDs) == 0 && len(m.Markers) == 0
}

// Lines describe what the migration changes, one per line.
func (m Migration) Lines() []string {
	var lines []string
	for _, id := range m.OldIDs {
		lines = append(lines, fmt.Sprintf("%s -> %s (%s): %d library mangas, %d downloads, %d cache entries (dropped)", id.Old, id.New, id.Name, id.Sources, id.Downloads, id.Cache))
	}
	for _, marker := range m.Markers {
		lines = append(lines, fmt.Sprintf("%s: %s -> %s", marker.Dir, marker.Old, marker.New))
	}
	return lines
}

// Apply moves the data to the current ids.
func (m Migration) Apply(fsys afero.Fs, cacheDB, dataDB *storage.DB) error {
	if err := storage.MigrateOldIDs(cacheDB, dataDB, m.OldIDs); err != nil {
		return err
	}
	for _, marker := range m.Markers {
		if err := UpdateMarker(fsys, marker); err != nil {
			return err
		}
	}
	return nil
}

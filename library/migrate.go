package library

import (
	"fmt"
	"io/fs"

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
	folders, err := markedDirs(fsys, dir, providerDirs)
	if err != nil {
		return nil, err
	}
	var markers []OldMarker
	for _, f := range folders {
		if loader, alias, ok := libmangal.FindLoader(loaders, f.info.Provider); ok && alias != nil {
			markers = append(markers, OldMarker{Dir: f.path, Old: f.info.Provider, New: loader.Info().ID})
		}
	}
	return markers, nil
}

// UpdateMarker writes the new provider id into the directory's info (with the mode).
func UpdateMarker(fsys afero.Fs, marker OldMarker, mode fs.FileMode) error {
	info, err := libmangal.ReadMangaDirInfo(fsys, marker.Dir)
	if err != nil {
		return err
	}
	info.Provider = marker.New
	if err := writeMangaDirInfo(fsys, marker.Dir, info, mode); err != nil {
		return fmt.Errorf("update %s: %w", marker.Dir, err)
	}
	return nil
}

// Migration is the data still under old provider ids, see PlanMigration.
type Migration struct {
	OldIDs  []storage.OldID
	Markers []OldMarker
}

// FindMigration finds the data under old provider ids: library entries, downloads and
// cache in the databases, and the .libmangal.json files under root.
func FindMigration(fsys afero.Fs, st storage.Storage, root string, providerDirs bool, loaders []libmangal.ProviderLoader) (Migration, error) {
	oldIDs, err := st.FindOldIDs(loaders)
	if err != nil {
		return Migration{}, err
	}
	markers, err := OldMarkers(fsys, root, providerDirs, loaders)
	if err != nil {
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

// ApplyTo moves the data to the current ids.
func (m Migration) ApplyTo(fsys afero.Fs, st storage.Storage, modes Modes) error {
	if err := st.MigrateOldIDs(m.OldIDs); err != nil {
		return err
	}
	for _, marker := range m.Markers {
		if err := UpdateMarker(fsys, marker, modes.File); err != nil {
			return err
		}
	}
	return nil
}

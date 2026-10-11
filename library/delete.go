package library

import (
	"fmt"
	"path/filepath"

	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

// DeleteManga moves the manga's folder to the trash and forgets it, see Files.DeleteManga.
//
// Deprecated: use Files.DeleteManga.
func DeleteManga(fsys afero.Fs, library storage.Library, root string, m storage.Manga) error {
	return Files{FS: fsys, Library: library, Root: root, Modes: DefaultModes}.DeleteManga(m)
}

// DeleteManga moves the manga's folder to the trash (only its recorded downloads when it
// shares the folder) and forgets it.
func (f Files) DeleteManga(m storage.Manga) error {
	batch := newTrashBatch()
	if m.Dir == "" {
		downloads, err := f.Library.Downloads(m.ID)
		if err != nil {
			return err
		}
		for _, dl := range downloads {
			if err := f.trashIfExists(batch, dl.Path); err != nil {
				return err
			}
		}
	} else if err := f.trashIfExists(batch, m.Path()); err != nil {
		return err
	}
	return f.Library.RemoveManga(m.ID)
}

// DeleteDownload moves a chapter file to the trash and forgets it, see Files.DeleteDownload.
//
// Deprecated: use Files.DeleteDownload.
func DeleteDownload(fsys afero.Fs, library storage.Library, root string, m storage.Manga, dl storage.Download) error {
	return Files{FS: fsys, Library: library, Root: root, Modes: DefaultModes}.DeleteDownload(m, dl)
}

// DeleteDownload moves a chapter file to the trash and forgets it, and removes the volume
// folder it was in if that's now empty.
func (f Files) DeleteDownload(m storage.Manga, dl storage.Download) error {
	if err := f.trashIfExists(newTrashBatch(), dl.Path); err != nil {
		return err
	}
	if err := f.Library.RemoveDownload(dl.Path); err != nil {
		return err
	}
	dir := filepath.Dir(dl.Path)
	if m.Dir == "" || dir == m.Path() {
		return nil
	}
	if empty, err := afero.IsEmpty(f.FS, dir); err == nil && empty {
		return f.FS.Remove(dir)
	}
	return nil
}

func (f Files) trashIfExists(batch, path string) error {
	exists, err := afero.Exists(f.FS, path)
	if err != nil || !exists {
		return err
	}
	if err := moveToTrash(f.FS, f.Root, batch, path, f.Modes); err != nil {
		return fmt.Errorf("trash %s: %w", path, err)
	}
	return nil
}

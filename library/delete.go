package library

import (
	"fmt"
	"path/filepath"

	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

// DeleteManga moves the manga's folder to the trash (only its recorded downloads when it
// shares the folder) and forgets it.
func DeleteManga(fsys afero.Fs, library storage.Library, root string, m storage.Manga) error {
	batch := newTrashBatch()
	if m.Dir == "" {
		downloads, err := library.Downloads(m.ID)
		if err != nil {
			return err
		}
		for _, dl := range downloads {
			if err := trashIfExists(fsys, root, batch, dl.Path); err != nil {
				return err
			}
		}
	} else if err := trashIfExists(fsys, root, batch, m.Path()); err != nil {
		return err
	}
	return library.RemoveManga(m.ID)
}

// DeleteDownload moves a chapter file to the trash and forgets it, and removes the volume
// folder it was in if that's now empty.
func DeleteDownload(fsys afero.Fs, library storage.Library, root string, m storage.Manga, dl storage.Download) error {
	if err := trashIfExists(fsys, root, newTrashBatch(), dl.Path); err != nil {
		return err
	}
	if err := library.RemoveDownload(dl.Path); err != nil {
		return err
	}
	dir := filepath.Dir(dl.Path)
	if m.Dir == "" || dir == m.Path() {
		return nil
	}
	if empty, err := afero.IsEmpty(fsys, dir); err == nil && empty {
		return fsys.Remove(dir)
	}
	return nil
}

func trashIfExists(fsys afero.Fs, root, batch, path string) error {
	exists, err := afero.Exists(fsys, path)
	if err != nil || !exists {
		return err
	}
	if err := moveToTrash(fsys, root, batch, path); err != nil {
		return fmt.Errorf("trash %s: %w", path, err)
	}
	return nil
}

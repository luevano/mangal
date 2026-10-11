package library

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/afero"
)

// TrashDir is the trash under the download directory: what merges replace goes there,
// for the user to empty.
const TrashDir = ".mangal-trash"

// TrashBatch is what was trashed at once (e.g. by a merge).
type TrashBatch struct {
	Name  string
	Path  string
	Files int
	Size  int64
}

// newTrashBatch names a batch after when it was made.
func newTrashBatch() string {
	return time.Now().Format("2006-01-02 15-04-05")
}

// moveToTrash moves path into the trash batch of root, keeping its path relative to root.
func moveToTrash(fsys afero.Fs, root, batch, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		rel = filepath.Base(path)
	}
	to := filepath.Join(root, TrashDir, batch, rel)
	if err := fsys.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	// a folder already in the batch (e.g. a volume with a trashed chapter) gets the rest
	if isDir, _ := afero.DirExists(fsys, path); isDir {
		if exists, _ := afero.DirExists(fsys, to); exists {
			entries, err := afero.ReadDir(fsys, path)
			if err != nil {
				return err
			}
			for _, e := range entries {
				if err := moveToTrash(fsys, root, batch, filepath.Join(path, e.Name())); err != nil {
					return err
				}
			}
			return fsys.Remove(path)
		}
	}
	return fsys.Rename(path, to)
}

// Trash lists the trash batches of root, oldest first.
func Trash(fsys afero.Fs, root string) ([]TrashBatch, error) {
	dir := filepath.Join(root, TrashDir)
	entries, err := afero.ReadDir(fsys, dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var batches []TrashBatch
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b := TrashBatch{Name: e.Name(), Path: filepath.Join(dir, e.Name())}
		err := afero.Walk(fsys, b.Path, func(_ string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				b.Files++
				b.Size += info.Size()
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		batches = append(batches, b)
	}
	return batches, nil
}

// EmptyTrash deletes everything in the trash of root.
func EmptyTrash(fsys afero.Fs, root string) error {
	return fsys.RemoveAll(filepath.Join(root, TrashDir))
}

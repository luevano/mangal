package library

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/luevano/mangal/v5/storage"
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

// moveToTrash moves path into the trash batch of root, keeping its path relative to root
// (outside root, its parent folder's name and its own). Nothing in the trash is
// overwritten: a name already taken gets a number.
func moveToTrash(ctx context.Context, fsys afero.Fs, j Journal, root, batch, path string, modes Modes) error {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		rel = filepath.Join(filepath.Base(filepath.Dir(path)), filepath.Base(path))
	}
	to := filepath.Join(root, TrashDir, batch, rel)
	isDir, err := afero.DirExists(fsys, path)
	if err != nil {
		return err
	}
	// a folder already in the batch (e.g. a volume with a trashed chapter) gets the rest
	if exists, _ := afero.DirExists(fsys, to); isDir && exists {
		entries, err := afero.ReadDir(fsys, path)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := moveToTrash(ctx, fsys, j, root, batch, filepath.Join(path, e.Name()), modes); err != nil {
				return err
			}
		}
		return removeDir(fsys, j, path)
	}
	to, err = freeName(fsys, to, isDir, nil)
	if err != nil {
		return err
	}
	return moveFile(ctx, fsys, j, path, to, modes)
}

// freeName is path, or path with a number (before a file's extension) if it's taken: it
// exists, or taken has it (may be nil).
func freeName(fsys afero.Fs, path string, isDir bool, taken map[string]bool) (string, error) {
	ext := ""
	if !isDir {
		ext = filepath.Ext(path)
	}
	base := strings.TrimSuffix(path, ext)
	for i := 2; ; i++ {
		exists, err := afero.Exists(fsys, path)
		if err != nil || !exists && !taken[path] {
			return path, err
		}
		path = fmt.Sprintf("%s (%d)%s", base, i, ext)
	}
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

// NeedTrash are the operations not undone yet whose undo needs files in the trash of
// root: emptying it blocks their undo.
func (u Undoer) NeedTrash(root string) ([]storage.Op, error) {
	ops, err := u.Journal.Ops(0)
	if err != nil {
		return nil, err
	}
	trash := filepath.Join(root, TrashDir)
	var need []storage.Op
	for _, op := range ops {
		if !op.Undone.IsZero() {
			continue
		}
		steps, err := u.Journal.Steps(op)
		if err != nil {
			return nil, err
		}
		for _, s := range steps {
			if _, in := under(trash, s.Dest); s.Kind == storage.StepMove && in {
				need = append(need, op)
				break
			}
		}
	}
	return need, nil
}

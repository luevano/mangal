package library

import (
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

// Journal records the file changes of an operation before they're made, so it can be
// undone (a *storage.Op). A nil Journal records nothing.
type Journal interface {
	Record(storage.Step) error
}

func logStep(j Journal, s storage.Step) error {
	if j == nil {
		return nil
	}
	return j.Record(s)
}

// mkdirAll creates dir and its missing parents, recording each one created.
func mkdirAll(fsys afero.Fs, j Journal, dir string, mode fs.FileMode) error {
	var missing []string
	for d := dir; ; d = filepath.Dir(d) {
		exists, err := afero.Exists(fsys, d)
		if err != nil {
			return err
		}
		if exists {
			break
		}
		missing = append(missing, d)
		if filepath.Dir(d) == d {
			break
		}
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := logStep(j, storage.Step{Kind: storage.StepMkdir, Path: missing[i]}); err != nil {
			return err
		}
	}
	return fsys.MkdirAll(dir, mode)
}

// removeDir removes an empty folder.
func removeDir(fsys afero.Fs, j Journal, dir string) error {
	if err := logStep(j, storage.Step{Kind: storage.StepRmdir, Path: dir}); err != nil {
		return err
	}
	return fsys.Remove(dir)
}

// writeDirInfo writes the folder's libmangal.MangaDirInfo, recording what it had.
func writeDirInfo(fsys afero.Fs, j Journal, dir string, info libmangal.MangaDirInfo, mode fs.FileMode) error {
	if j != nil {
		path := filepath.Join(dir, libmangal.FilenameMangaDirInfo)
		old, err := readIfExists(fsys, path)
		if err != nil {
			return err
		}
		// as libmangal writes it; undo compares them as JSON anyway
		data, err := json.MarshalIndent(info, "", "  ")
		if err != nil {
			return err
		}
		if err := j.Record(storage.Step{Kind: storage.StepWrite, Path: path, Old: old, New: data}); err != nil {
			return err
		}
	}
	return libmangal.WriteMangaDirInfo(fsys, dir, info, mode)
}

// readIfExists is the file's content, nil if it doesn't exist (empty, not nil, if it's
// empty).
func readIfExists(fsys afero.Fs, path string) ([]byte, error) {
	data, err := afero.ReadFile(fsys, path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, err
	case data == nil:
		return []byte{}, nil
	}
	return data, nil
}

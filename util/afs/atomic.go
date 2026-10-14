package afs

import (
	"io/fs"
	"path/filepath"
)

// WriteFileAtomic replaces the file with data through a synced temporary file renamed over
// it, so a crash never leaves it half-written. An existing file keeps its mode (a new one
// gets perm) and a symlinked one stays a symlink; a missing directory is made 0700.
func WriteFileAtomic(path string, data []byte, perm fs.FileMode) error {
	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	}
	if info, err := Afero.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}
	dir := filepath.Dir(path)
	if err := Afero.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := Afero.TempFile(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer Afero.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := Afero.Chmod(tmp.Name(), perm); err != nil {
		return err
	}
	return Afero.Rename(tmp.Name(), path)
}

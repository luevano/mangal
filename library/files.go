package library

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

// Modes are the permissions of the files and folders the library creates
// (download.mode_file and download.mode_dir).
type Modes struct {
	File, Dir fs.FileMode
}

// DefaultModes are the defaults of download.mode_file and download.mode_dir.
var DefaultModes = Modes{File: 0o644, Dir: 0o755}

// Files changes the library's files and keeps the library index in sync.
type Files struct {
	FS      afero.Fs
	Library storage.Library
	// Root is the download directory, with the trash.
	Root  string
	Modes Modes
	// OnProgress, if set, is told where Mark and ApplyMerge are.
	OnProgress func(Progress)
	// Journal, if set, records the file changes, for undo.
	Journal Journal
}

// moveFile renames a file or folder, copying it (and removing the original) when it's on
// another drive. It never replaces what's at to (a rename would replace a file or an
// empty folder): that's an fs.ErrExist.
func moveFile(fsys afero.Fs, j Journal, from, to string, modes Modes) error {
	if err := mkdirAll(fsys, j, filepath.Dir(to), modes.Dir); err != nil {
		return err
	}
	if _, err := lstat(fsys, to); err == nil {
		return fmt.Errorf("move %s: %s: %w", from, to, fs.ErrExist)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := logStep(j, storage.Step{Kind: storage.StepMove, Path: from, Dest: to}); err != nil {
		return err
	}
	err := fsys.Rename(from, to)
	if !crossDevice(err) {
		return err
	}
	created, err := copyAll(fsys, from, to)
	if err != nil {
		if created {
			// only what the copy made: something there before is never removed
			_ = fsys.RemoveAll(to)
		}
		return err
	}
	return fsys.RemoveAll(from)
}

// lstat is os.Lstat when the filesystem has it.
func lstat(fsys afero.Fs, path string) (fs.FileInfo, error) {
	if a, ok := fsys.(afero.Afero); ok {
		fsys = a.Fs
	}
	if l, ok := fsys.(afero.Lstater); ok {
		info, _, err := l.LstatIfPossible(path)
		return info, err
	}
	return fsys.Stat(path)
}

// renameCase renames from to to, the same file when only the case changes on a
// case-insensitive filesystem, through a temporary name (back to from if the second
// rename fails).
func renameCase(fsys afero.Fs, j Journal, from, to string) error {
	if err := logStep(j, storage.Step{Kind: storage.StepMove, Path: from, Dest: to}); err != nil {
		return err
	}
	tmp := from + ".mangal-rename"
	if err := fsys.Rename(from, tmp); err != nil {
		return err
	}
	if err := fsys.Rename(tmp, to); err != nil {
		if backErr := fsys.Rename(tmp, from); backErr != nil {
			return fmt.Errorf("%w (and renaming %s back: %w)", err, tmp, backErr)
		}
		return err
	}
	return nil
}

func crossDevice(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	// ERROR_NOT_SAME_DEVICE
	return errno == syscall.EXDEV || runtime.GOOS == "windows" && errno == 17
}

// copyAll copies a file or folder, keeping the permissions. created is true once it
// made to (it fails when to exists).
func copyAll(fsys afero.Fs, from, to string) (created bool, err error) {
	err = afero.Walk(fsys, from, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, rel)
		switch {
		case rel == "." && info.IsDir():
			if err := fsys.Mkdir(target, info.Mode().Perm()); err != nil {
				return err
			}
			created = true
			return nil
		case info.IsDir():
			return fsys.MkdirAll(target, info.Mode().Perm())
		}
		err = copyFile(fsys, path, target, info.Mode().Perm())
		if rel == "." && err == nil {
			created = true
		}
		return err
	})
	return created, err
}

func copyFile(fsys afero.Fs, from, to string, mode fs.FileMode) error {
	src, err := fsys.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := fsys.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	if closeErr := dst.Close(); err == nil {
		err = closeErr
	}
	return err
}

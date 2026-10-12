package library

import (
	"errors"
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
}

// moveFile renames a file or folder, copying it (and removing the original) when it's on
// another drive.
func moveFile(fsys afero.Fs, from, to string, modes Modes) error {
	if err := fsys.MkdirAll(filepath.Dir(to), modes.Dir); err != nil {
		return err
	}
	err := fsys.Rename(from, to)
	if !crossDevice(err) {
		return err
	}
	if err := copyAll(fsys, from, to); err != nil {
		_ = fsys.RemoveAll(to)
		return err
	}
	return fsys.RemoveAll(from)
}

func crossDevice(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	// ERROR_NOT_SAME_DEVICE
	return errno == syscall.EXDEV || runtime.GOOS == "windows" && errno == 17
}

// copyAll copies a file or folder, keeping the permissions.
func copyAll(fsys afero.Fs, from, to string) error {
	return afero.Walk(fsys, from, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, rel)
		if info.IsDir() {
			return fsys.MkdirAll(target, info.Mode().Perm())
		}
		return copyFile(fsys, path, target, info.Mode().Perm())
	})
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

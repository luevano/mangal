// Package path has mangal's directories. The getters only build the paths, Ensure creates
// them.
package path

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/adrg/xdg"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/meta"
	"github.com/luevano/mangal/v5/util/afs"
)

type PathName string

const (
	PathCache     PathName = "cache"
	PathConfig    PathName = "config"
	PathDownloads PathName = "downloads"
	PathTemp      PathName = "temp"
	PathProviders PathName = "providers"
	PathLog       PathName = "log"
	PathData      PathName = "data"
)

type Paths map[PathName]string

// Keys are the path names, sorted.
func (p Paths) Keys() []PathName {
	return slices.Sorted(maps.Keys(p))
}

func (p Paths) Get(name PathName) string {
	return p[name]
}

func (p Paths) GetAsPaths(name PathName) Paths {
	return Paths{name: p.Get(name)}
}

// All returns every path, none is created.
func All() Paths {
	return Paths{
		PathCache:     Cache(),
		PathConfig:    Config(),
		PathDownloads: Downloads(),
		PathTemp:      Temp(),
		PathProviders: Providers(),
		PathLog:       Logs(),
		PathData:      Data(),
	}
}

// Cache is where the cache database is (cache.path).
func Cache() string {
	return config.Cache.Path.Get()
}

// Data is where mangal keeps data that isn't cache (e.g. its database).
func Data() string {
	return filepath.Join(xdg.DataHome, meta.AppName)
}

// Config is the directory of the config file.
func Config() string {
	return filepath.Dir(config.Path)
}

// Downloads is download.path.
func Downloads() string {
	return config.Download.Path.Get()
}

// Providers is where the Lua providers are (providers.path).
func Providers() string {
	return config.Providers.Path.Get()
}

// Logs is where the log files are.
func Logs() string {
	return filepath.Join(xdg.StateHome, meta.AppName, "logs")
}

// Temp is the user's temporary directory of mangal (e.g. chapters downloaded for reading),
// create it with EnsureTemp.
func Temp() string {
	name := meta.AppName
	// os.TempDir is shared by every user on unix
	if uid := os.Getuid(); uid != -1 {
		name += "-" + strconv.Itoa(uid)
	}
	return filepath.Join(os.TempDir(), name)
}

// Ensure creates the directory if it doesn't exist (with download.mode_dir_decimal) and
// returns it.
func Ensure(dir string) (string, error) {
	if err := afs.Afero.MkdirAll(dir, config.Download.ModeDir.Get()); err != nil {
		return dir, fmt.Errorf("create directory: %w", err)
	}
	return dir, nil
}

// EnsureTemp creates Temp, only accessible by the user, and returns it. It errors if it
// exists but isn't a directory only the user can write to (e.g. another user made it).
func EnsureTemp() (string, error) {
	dir := Temp()
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return dir, fmt.Errorf("create temp directory: %w", err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return dir, fmt.Errorf("create temp directory: %w", err)
	}
	if !info.IsDir() || info.Mode().Perm()&0o022 != 0 {
		return dir, fmt.Errorf("temp directory %q isn't a directory only you can write to (%s), remove it", dir, info.Mode())
	}
	return dir, nil
}

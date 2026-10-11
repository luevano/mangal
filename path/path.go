package path

import (
	"github.com/adrg/xdg"
	"os"
	"path/filepath"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/meta"
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

func (p Paths) Keys() []PathName {
	keys := make([]PathName, len(p))
	i := 0
	for k := range p {
		keys[i] = k
		i++
	}
	return keys
}

func (p Paths) Get(name PathName) string {
	return p[name]
}

func (p Paths) GetAsPaths(name PathName) Paths {
	return Paths{name: p.Get(name)}
}

func AllPaths() Paths {
	return Paths{
		PathCache:     CacheDir(),
		PathConfig:    ConfigDir(),
		PathDownloads: DownloadsDir(),
		PathTemp:      TempDir(),
		PathProviders: ProvidersDir(),
		PathLog:       LogDir(),
		PathData:      DataDir(),
	}
}

func CacheDir() string {
	dir := config.Cache.Path.Get()
	createDirIfAbsent(dir)
	return dir
}

// DataDir is where mangal keeps data that isn't cache (e.g. its database).
func DataDir() string {
	dir := filepath.Join(xdg.DataHome, meta.AppName)
	createDirIfAbsent(dir)
	return dir
}

func ConfigDir() string {
	var dir string = filepath.Dir(config.Path)
	createDirIfAbsent(dir)
	return dir
}

func DownloadsDir() string {
	dir := config.Download.Path.Get()
	createDirIfAbsent(dir)
	return dir
}

func TempDir() string {
	dir := filepath.Join(os.TempDir(), meta.AppName)
	createDirIfAbsent(dir)
	return dir
}

func ProvidersDir() string {
	dir := config.Providers.Path.Get()
	createDirIfAbsent(dir)
	return dir
}

func LogDir() string {
	dir := filepath.Join(TempDir(), "logs")
	createDirIfAbsent(dir)
	return dir
}

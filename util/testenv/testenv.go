// Package testenv keeps tests away from the user's files: under go test, the xdg
// directories (home, config, data, state, cache, downloads) are moved to a temp dir of the
// test process before any package reads them, and the system keyring is off. Imported by
// config, so every package that can reach the user's files has it.
package testenv

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/adrg/xdg"
)

// Dir is the temp dir of the test process (made when something writes there), empty
// outside tests.
var Dir string

func init() {
	if !testing.Testing() {
		return
	}
	Dir = filepath.Join(os.TempDir(), "mangal-test-"+strconv.Itoa(os.Getpid()))
	xdg.Home = filepath.Join(Dir, "home")
	xdg.ConfigHome = filepath.Join(Dir, "config")
	xdg.DataHome = filepath.Join(Dir, "data")
	xdg.StateHome = filepath.Join(Dir, "state")
	xdg.CacheHome = filepath.Join(Dir, "cache")
	xdg.UserDirs.Download = filepath.Join(Dir, "downloads")
	if _, ok := os.LookupEnv("MANGAL_AUTH_KEYRING"); !ok {
		os.Setenv("MANGAL_AUTH_KEYRING", "false")
	}
}

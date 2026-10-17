// Package testenv keeps tests away from the user's files: under go test, the xdg
// directories (home, config, data, state, cache, downloads) are moved to a temp dir of the
// test process before any package reads them, and the system keyring is off. Imported by
// config, so every package that can reach the user's files has it.
package testenv

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/adrg/xdg"
	"github.com/luevano/mangal/v5/util/proc"
)

const dirPrefix = "mangal-test-"

// Dir is the temp dir of the test process (made when something writes there), empty
// outside tests.
var Dir string

func init() {
	if !testing.Testing() {
		return
	}
	removeStale()
	Dir = filepath.Join(os.TempDir(), dirPrefix+strconv.Itoa(os.Getpid()))
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

// removeStale removes the dirs of test processes that ended: there's no exit hook for a
// package, so each process cleans up after the ones before it.
func removeStale() {
	dirs, _ := filepath.Glob(filepath.Join(os.TempDir(), dirPrefix+"*"))
	for _, d := range dirs {
		pid, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(d), dirPrefix))
		if err != nil || pid == os.Getpid() || proc.Alive(pid) {
			continue
		}
		_ = os.RemoveAll(d)
	}
}

package testenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrg/xdg"
)

func TestAwayFromTheUser(t *testing.T) {
	if Dir == "" || !strings.HasPrefix(Dir, os.TempDir()) {
		t.Fatalf("dir %q", Dir)
	}
	for _, d := range []string{xdg.Home, xdg.ConfigHome, xdg.DataHome, xdg.StateHome, xdg.CacheHome, xdg.UserDirs.Download} {
		if !strings.HasPrefix(d, Dir+string(filepath.Separator)) {
			t.Errorf("%s isn't under %s", d, Dir)
		}
	}
}

func TestRemoveStale(t *testing.T) {
	// a pid no process has (above the default pid_max)
	stale := filepath.Join(os.TempDir(), dirPrefix+"99999999")
	if err := os.MkdirAll(filepath.Join(stale, "home"), 0o700); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(os.TempDir(), dirPrefix+"notapid")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(other) })
	removeStale()
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale dir kept: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("a dir that isn't a test process's was removed: %v", err)
	}
}

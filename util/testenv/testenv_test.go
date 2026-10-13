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

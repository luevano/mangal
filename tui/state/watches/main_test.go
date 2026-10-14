package watches

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
)

func TestMain(m *testing.M) {
	// the dbs open once per process: before any test, keep them away from the user's
	dir, err := os.MkdirTemp("", "mangal-watches-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("MANGAL_CACHE_PATH", filepath.Join(dir, "cache"))
	os.Setenv("MANGAL_AUTH_KEYRING", "false")
	os.Setenv("MANGAL_PROVIDERS_PATH", filepath.Join(dir, "providers"))
	xdg.DataHome = filepath.Join(dir, "data")
	xdg.StateHome = filepath.Join(dir, "state")
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

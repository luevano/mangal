package serve

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
)

func TestMain(m *testing.M) {
	// nothing here may open the user's databases
	dir, err := os.MkdirTemp("", "mangal-serve-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("MANGAL_CACHE_PATH", filepath.Join(dir, "cache"))
	os.Setenv("MANGAL_AUTH_KEYRING", "false")
	xdg.DataHome = filepath.Join(dir, "data")
	xdg.StateHome = filepath.Join(dir, "state")
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

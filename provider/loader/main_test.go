package loader

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/adrg/xdg"
	"github.com/luevano/mangal/v5/config"
)

func TestMain(m *testing.M) {
	// the dbs open once per process: before any test, keep them away from the user's
	dir, err := os.MkdirTemp("", "mangal-loader-test-")
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

func TestMangoOptionsMangaDexGroups(t *testing.T) {
	t.Setenv("MANGAL_PROVIDERS_MANGAPLUS_SECRET_FILE", "")
	if err := config.Providers.MangaDex.ExcludeGroups.Set([]string{"Bad Group"}); err != nil {
		t.Fatal(err)
	}
	if err := config.Providers.MangaDex.PreferGroups.Set([]string{"First", "Second"}); err != nil {
		t.Fatal(err)
	}
	o := mangoOptions()
	if !slices.Equal(o.MangaDex.ExcludeGroups, []string{"Bad Group"}) || !slices.Equal(o.MangaDex.PreferGroups, []string{"First", "Second"}) {
		t.Errorf("exclude %q, prefer %q", o.MangaDex.ExcludeGroups, o.MangaDex.PreferGroups)
	}
}

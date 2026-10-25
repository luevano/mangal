package loader

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
	"github.com/luevano/libmangal"
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

func TestMangoOptionsSettings(t *testing.T) {
	loaders, err := MangoLoaders()
	if err != nil {
		t.Fatal(err)
	}
	var infos []libmangal.ProviderInfo
	for _, l := range loaders {
		infos = append(infos, l.Info())
	}
	config.RegisterProviders(infos)
	defer config.RegisterProviders(nil)
	t.Setenv("MANGAL_PROVIDERS_MANGO_MANGADEX_PREFER_GROUPS", "First, Second")
	t.Setenv("MANGAL_PROVIDERS_MANGO_MANGAPLUS_API", "web")
	o := mangoOptions()
	if o.Settings == nil {
		t.Fatal("no settings hook")
	}
	if got := o.Settings("mango-mangadex")["prefer_groups"]; got != "First, Second" {
		t.Errorf("prefer_groups %q", got)
	}
	plus := o.Settings("mango-mangaplus")
	if plus["api"] != "web" || plus["quality"] != "super_high" || plus["request_interval"] != "500ms" {
		t.Errorf("mangaplus %v", plus)
	}
}

package client

import (
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/config"
)

func TestMetadataProvidersOrder(t *testing.T) {
	// the providers open their cache buckets, away from the user's databases
	dir := t.TempDir()
	t.Setenv("MANGAL_CACHE_PATH", filepath.Join(dir, "cache"))
	t.Setenv("MANGAL_AUTH_KEYRING", "false")
	dataHome := xdg.DataHome
	xdg.DataHome = filepath.Join(dir, "data")
	t.Cleanup(func() {
		xdg.DataHome = dataHome
		_ = config.Metadata.Kitsu.Enabled.Set(true)
	})

	sources := func() []metadata.IDSource {
		t.Helper()
		metas, err := metadataProviders()
		if err != nil {
			t.Fatal(err)
		}
		var got []metadata.IDSource
		for _, m := range metas {
			got = append(got, m.Info().Source)
		}
		return got
	}

	if got := sources(); len(got) != 2 || got[0] != metadata.IDSourceAnilist || got[1] != metadata.IDSourceKitsu {
		t.Errorf("default providers %v, want anilist then kitsu", got)
	}
	if err := config.Metadata.Kitsu.Enabled.Set(false); err != nil {
		t.Fatal(err)
	}
	if got := sources(); len(got) != 1 || got[0] != metadata.IDSourceAnilist {
		t.Errorf("providers without kitsu %v", got)
	}
}

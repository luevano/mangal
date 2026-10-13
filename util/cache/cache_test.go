package cache

import (
	"path/filepath"
	"testing"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/libmangal/metadata/anilist"
	"github.com/luevano/libmangal/metadata/kitsu"
	"github.com/luevano/libmangal/metadata/myanimelist"
	"github.com/luevano/mangal/v5/storage"
)

// cached metadata, users and tokens are stored as interfaces: their types must be registered
// with gob (libmangal does it for its metadata providers)
func TestCachedInterfacesDecode(t *testing.T) {
	db, err := storage.OpenCache(filepath.Join(t.TempDir(), storage.FilenameCache))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	st := storage.Storage{Cache: db}
	store := st.Store("anilist", metadata.CacheBucketNameIDToManga, storage.StoreOptions{})

	for _, in := range []metadata.Metadata{&anilist.Manga{IDProvider: 1}, &myanimelist.Manga{IDProvider: 2}, &kitsu.Manga{IDProvider: 3}} {
		if err := store.Set("m", &in); err != nil {
			t.Fatalf("set %T: %v", in, err)
		}
		var out metadata.Metadata
		if found, err := store.Get("m", &out); err != nil || !found {
			t.Fatalf("get %T: %v (found %v)", in, err, found)
		}
		if out.ID() != in.ID() {
			t.Errorf("%T: got %T %v", in, out, out.ID())
		}
	}
	for _, in := range []any{&anilist.User{}, &myanimelist.User{}, &kitsu.User{}, &kitsu.Token{AccessToken: "a"}} {
		if err := store.Set("v", &in); err != nil {
			t.Fatalf("set %T: %v", in, err)
		}
		var out any
		if found, err := store.Get("v", &out); err != nil || !found {
			t.Fatalf("get %T: %v (found %v)", in, err, found)
		}
	}
}

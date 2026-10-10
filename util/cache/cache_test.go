package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheDirMovesOldMetadataDB(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "anilist.db"), []byte("bindings"), 0o600); err != nil {
		t.Fatal(err)
	}

	dir, err := cacheDir(root, "anilist")
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(root, metadataDir) {
		t.Errorf("dir %q", dir)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "anilist.db")); err != nil || string(b) != "bindings" {
		t.Errorf("moved db: %q, %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(root, "anilist.db")); !os.IsNotExist(err) {
		t.Error("old db still in the root")
	}

	// a db already in place isn't replaced
	_ = os.WriteFile(filepath.Join(root, "anilist.db"), []byte("older"), 0o600)
	if _, err := cacheDir(root, "anilist"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "anilist.db")); string(b) != "bindings" {
		t.Errorf("replaced with %q", b)
	}

	if dir, _ := cacheDir(root, "mango-mangadex"); dir != root {
		t.Errorf("provider db in %q", dir)
	}
}

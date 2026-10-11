package path

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/luevano/mangal/v5/config"
)

func TestGettersDontCreate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dl")
	if err := config.Download.Path.Set(dir); err != nil {
		t.Fatal(err)
	}
	if got := Downloads(); got != dir {
		t.Fatalf("got %q", got)
	}
	_ = All()
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("getter created the dir")
	}
	if _, err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("not created: %v", err)
	}
}

func TestEnsureTemp(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	dir, err := EnsureTemp()
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("%v %v", info.Mode(), err)
	}
	// someone else's, writable by others
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureTemp(); err == nil {
		t.Error("world-writable temp dir accepted")
	}
	os.Remove(dir)
	if err := os.Symlink(t.TempDir(), dir); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureTemp(); err == nil {
		t.Error("symlink accepted")
	}
}

func TestKeysSorted(t *testing.T) {
	keys := All().Keys()
	for i := 1; i < len(keys); i++ {
		if keys[i-1] > keys[i] {
			t.Fatalf("not sorted: %v", keys)
		}
	}
}

func TestDataPath(t *testing.T) {
	if got := Data(); filepath.Base(got) != "mangal" {
		t.Errorf("default %q", got)
	}
	dir := filepath.Join(t.TempDir(), "data2")
	if err := config.Data.Path.Set(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = config.Data.Path.Set("") }()
	if got := Data(); got != dir {
		t.Errorf("got %q, want data.path %q", got, dir)
	}
}

package path

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestExistingDownloads(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dl")
	if err := config.Download.Path.Set(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := ExistingDownloads(); !errors.Is(err, ErrNoDownloads) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("created the dir")
	}
	if err := os.WriteFile(dir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ExistingDownloads(); err == nil {
		t.Fatal("a file")
	}
	_ = os.Remove(dir)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if got, err := ExistingDownloads(); err != nil || got != dir {
		t.Fatalf("%q %v", got, err)
	}
}

// the tests' library operations snapshot and journal in a temp dir, not the user's data
func TestAwayFromTheUser(t *testing.T) {
	for _, d := range []string{Data(), Logs(), Config(), config.Cache.Path.Default, config.Download.Path.Default} {
		if !strings.HasPrefix(d, filepath.Join(os.TempDir(), "mangal-test-")) {
			t.Errorf("%s isn't in a test dir", d)
		}
	}
}

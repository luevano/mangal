package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrg/xdg"
)

func TestPathEntryExpandsHome(t *testing.T) {
	if err := Library.Path.Set("~/manga"); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(xdg.Home, "manga")
	if got := Library.Path.Get(); got != want {
		t.Errorf("wanted %q, got %q", want, got)
	}
}

func TestWriteKeyOnlyWritesThatKey(t *testing.T) {
	Path = filepath.Join(t.TempDir(), "mangal.toml")
	if err := os.WriteFile(Path, []byte("[download.metadata]\nsearch = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// as a flag would do
	if err := Download.Metadata.Search.Set(false); err != nil {
		t.Fatal(err)
	}

	if err := WriteKey(Providers.MangaPlus.FreeSecret.Key, "free-secret"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(Path)
	if err != nil {
		t.Fatal(err)
	}
	file := string(data)
	if !strings.Contains(file, "free_secret = 'free-secret'") {
		t.Errorf("key not written:\n%s", file)
	}
	if !strings.Contains(file, "search = true") {
		t.Errorf("runtime value was written:\n%s", file)
	}
	if strings.Contains(file, "viewer") {
		t.Errorf("defaults were written:\n%s", file)
	}
}

func TestWriteKeyDoesNotCreateConfig(t *testing.T) {
	Path = filepath.Join(t.TempDir(), "mangal.toml")

	if err := WriteKey(Providers.MangaPlus.FreeSecret.Key, "free-secret"); err == nil {
		t.Error("wanted an error without a config file")
	}
	if _, err := os.Stat(Path); err == nil {
		t.Error("config file was created")
	}
}

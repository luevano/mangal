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

func TestEnvOverride(t *testing.T) {
	Init()
	t.Setenv("MANGAL_DOWNLOAD_MANGA_NAME_TEMPLATE", "{{ .Manga.Title }}")
	if got := Download.Manga.NameTemplate.Get(); got != "{{ .Manga.Title }}" {
		t.Errorf("got %q, want the env value", got)
	}
}

func TestMirrors(t *testing.T) {
	Path = filepath.Join(t.TempDir(), "mangal.toml")
	toml := "[providers.mirrors]\nmango-mangakakalot = 'https://www.mangakakalove.com/'\n"
	if err := os.WriteFile(Path, []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Load(Path); err != nil {
		t.Fatal(err)
	}
	if got := Providers.Mirrors.Get()["mango-mangakakalot"]; got != "https://www.mangakakalove.com/" {
		t.Fatalf("got %q", got)
	}

	if err := SetMirror("mango-manganato", "https://www.nelomanga.net/"); err != nil {
		t.Fatal(err)
	}
	if err := SetMirror("mango-mangakakalot", ""); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(Path)
	if file := string(data); !strings.Contains(file, "mango-manganato = 'https://www.nelomanga.net/'") || strings.Contains(file, "mangakakalove") {
		t.Errorf("want only the manganato mirror:\n%s", file)
	}

	if err := Validate(Providers.Mirrors.Key, map[string]any{"mango-x": "not a url"}); err == nil {
		t.Error("invalid mirror: no error")
	}
}

package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrg/xdg"
	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func TestMain(m *testing.M) {
	// the developer's MANGAL_* variables must not change the results
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "MANGAL_") {
			os.Unsetenv(name)
		}
	}
	os.Exit(m.Run())
}

// reset starts from the defaults, with the config file (absent) in a temp dir.
func reset(t *testing.T) {
	t.Helper()
	viper.Reset()
	setup()
	dir = t.TempDir()
	Path = filepath.Join(dir, filename)
	unsupported = nil
}

func writeConfig(t *testing.T, toml string) {
	t.Helper()
	if err := os.WriteFile(Path, []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readConfig(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(Path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestDefaults(t *testing.T) {
	reset(t)
	if err := Validate(); err != nil {
		t.Fatal(err)
	}
	if got := Providers.Parallelism.Get(); got != 15 {
		t.Errorf("parallelism %d", got)
	}
	if got := Icons.Get(); got != icon.TypeASCII {
		t.Errorf("icons %s", got)
	}
}

func TestPathEntryExpandsHome(t *testing.T) {
	reset(t)
	if err := Download.Path.Set("~/manga"); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(xdg.Home, "manga")
	if got := Download.Path.Get(); got != want {
		t.Errorf("wanted %q, got %q", want, got)
	}
	// the raw value keeps the ~
	if got, _ := Get(Download.Path.Key); got != "~/manga" {
		t.Errorf("raw %v", got)
	}
}

func TestEnv(t *testing.T) {
	reset(t)
	t.Setenv("MANGAL_DOWNLOAD_MANGA_NAME_TEMPLATE", "{{ .Manga.Title }}")
	t.Setenv("MANGAL_PROVIDERS_PARALLELISM", "5")
	t.Setenv("MANGAL_DOWNLOAD_MODE_DIR_DECIMAL", "448")
	t.Setenv("MANGAL_READ_FORMAT", "cbz")
	t.Setenv("MANGAL_ICONS", "nerd")
	t.Setenv("MANGAL_TUI_SKIP_HOME", "false")
	t.Setenv("MANGAL_PROVIDERS_MIRRORS", `{"mango-x": "https://x.example/"}`)
	if err := Validate(); err != nil {
		t.Fatal(err)
	}
	if got := Download.Manga.NameTemplate.Get(); got != "{{ .Manga.Title }}" {
		t.Errorf("template %q", got)
	}
	if got := Providers.Parallelism.Get(); got != 5 {
		t.Errorf("parallelism %d", got)
	}
	if got := Download.ModeDir.Get(); got != 0o700 {
		t.Errorf("mode %o", got)
	}
	if got := Read.Format.Get(); got != libmangal.FormatCBZ {
		t.Errorf("format %s", got)
	}
	if got := Icons.Get(); got != icon.TypeNerd {
		t.Errorf("icons %s", got)
	}
	if TUI.SkipHome.Get() {
		t.Error("skip home")
	}
	if got := Providers.Mirrors.Get()["mango-x"]; got != "https://x.example/" {
		t.Errorf("mirror %q", got)
	}
}

func TestInvalidEnv(t *testing.T) {
	for name, value := range map[string]string{
		"MANGAL_READ_FORMAT":           "bad",
		"MANGAL_ICONS":                 "x",
		"MANGAL_PROVIDERS_PARALLELISM": "300",
		"MANGAL_TUI_SKIP_HOME":         "maybe",
		"MANGAL_CACHE_TTL":             "1 day",
		"MANGAL_PROVIDERS_MIRRORS":     "x",
	} {
		t.Run(name, func(t *testing.T) {
			reset(t)
			t.Setenv(name, value)
			err := Validate()
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("got %v", err)
			}
		})
	}
	// a bad value can't come out of Get, the default does
	reset(t)
	t.Setenv("MANGAL_PROVIDERS_PARALLELISM", "300")
	if got := Providers.Parallelism.Get(); got != 15 {
		t.Errorf("parallelism %d", got)
	}
}

func TestFlags(t *testing.T) {
	reset(t)
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.Uint8("parallelism", 0, "")
	fs.String("format", "", "")
	if err := BindPFlag(Providers.Parallelism.Key, fs.Lookup("parallelism")); err != nil {
		t.Fatal(err)
	}
	if err := BindPFlag(Download.Format.Key, fs.Lookup("format")); err != nil {
		t.Fatal(err)
	}
	if err := BindPFlag("nope", fs.Lookup("format")); !errors.Is(err, ErrUnknownKey) {
		t.Errorf("unknown key: %v", err)
	}
	// not given, the config applies
	if got := Providers.Parallelism.Get(); got != 15 {
		t.Errorf("parallelism %d", got)
	}
	if err := fs.Parse([]string{"--parallelism", "7", "--format", "bad"}); err != nil {
		t.Fatal(err)
	}
	if got := Providers.Parallelism.Get(); got != 7 {
		t.Errorf("parallelism %d", got)
	}
	if err := Validate(); err == nil || !strings.Contains(err.Error(), Download.Format.Key) {
		t.Errorf("bad format flag: %v", err)
	}
}

func TestParse(t *testing.T) {
	reset(t)
	tests := []struct {
		key, value string
		want       any
	}{
		{"providers.parallelism", "20", int64(20)},
		{"download.mode_dir_decimal", "493", int64(493)},
		{"icons", "nerd", "nerd"},
		{"download.format", "cbz", "cbz"},
		{"tui.skip_home", "false", false},
		{"download.path", "~/dl", "~/dl"},
	}
	for _, tt := range tests {
		got, err := Parse(tt.key, tt.value)
		if err != nil || got != tt.want {
			t.Errorf("Parse(%q, %q) = %v (%T), %v", tt.key, tt.value, got, got, err)
		}
	}
	for key, value := range map[string]string{
		"providers.parallelism":            "0",
		"download.format":                  "bad",
		"icons":                            "x",
		"providers.mangaplus.quality":      "max",
		"notification.discord.webhook_url": "https://example.com",
		"download.path":                    "",
	} {
		if _, err := Parse(key, value); err == nil {
			t.Errorf("Parse(%q, %q): no error", key, value)
		}
	}
	if _, err := Parse("nope", "x"); !errors.Is(err, ErrUnknownKey) {
		t.Errorf("unknown key: %v", err)
	}
	if _, err := Get("nope"); !errors.Is(err, ErrUnknownKey) {
		t.Errorf("unknown key: %v", err)
	}
}

func TestLoad(t *testing.T) {
	reset(t)
	writeConfig(t, "icons = 'Nerd'\nold_key = 1\n[download]\nmode_db_decimal = 384\n[library]\npath = ''\n[providers.mirrors]\nmango-x = 'https://x.example/'\n")
	if err := Load(Path); err != nil {
		t.Fatal(err)
	}
	if got := Icons.Get(); got != icon.TypeNerd {
		t.Errorf("icons %s", got)
	}
	if got := Unsupported(); len(got) != 1 || got[0] != "old_key" {
		t.Errorf("unsupported %q", got)
	}
	// config write drops the retired keys, keeps the rest
	if err := Write(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "mode_db") || strings.Contains(string(data), "[library]") || !strings.Contains(string(data), "old_key") {
		t.Errorf("written:\n%s", data)
	}

	reset(t)
	if err := Load(filepath.Join(dir, "missing.toml")); err == nil {
		t.Error("explicit missing file: no error")
	}
	reset(t)
	if err := Load(""); err != nil {
		t.Errorf("default missing file: %v", err)
	}
}

func TestWriteKeyOnlyWritesThatKey(t *testing.T) {
	reset(t)
	writeConfig(t, "[download.metadata]\nsearch = true\n")
	// as a flag would do
	if err := Download.Metadata.Search.Set(false); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MANGAL_DOWNLOAD_PATH", "/env/path")

	if err := WriteKey(Providers.MangaPlus.FreeSecret.Key, "free-secret"); err != nil {
		t.Fatal(err)
	}
	if err := Download.Format.Write(libmangal.FormatCBZ); err != nil {
		t.Fatal(err)
	}

	file := readConfig(t)
	for _, want := range []string{"free_secret = 'free-secret'", "format = 'CBZ'", "search = true"} {
		if !strings.Contains(file, want) {
			t.Errorf("no %q in:\n%s", want, file)
		}
	}
	for _, unwanted := range []string{"viewer", "/env/path"} {
		if strings.Contains(file, unwanted) {
			t.Errorf("%q written:\n%s", unwanted, file)
		}
	}
	if got := Download.Format.Get(); got != libmangal.FormatCBZ {
		t.Errorf("format not set: %s", got)
	}
	if err := WriteKey(Providers.Parallelism.Key, "300"); err == nil {
		t.Error("invalid value written")
	}
}

func TestWriteKeyDoesNotCreateConfig(t *testing.T) {
	reset(t)
	if err := WriteKey(Providers.MangaPlus.FreeSecret.Key, "free-secret"); err == nil {
		t.Error("wanted an error without a config file")
	}
	if _, err := os.Stat(Path); err == nil {
		t.Error("config file was created")
	}
}

func TestCreateFile(t *testing.T) {
	reset(t)
	Path = filepath.Join(dir, "sub", filename)
	if err := CreateFile(); err != nil {
		t.Fatal(err)
	}
	if err := WriteKey(Icons.Key, "nerd"); err != nil {
		t.Fatal(err)
	}
	if file := readConfig(t); strings.TrimSpace(file) != "icons = 'nerd'" {
		t.Errorf("file:\n%s", file)
	}
}

func TestWrite(t *testing.T) {
	reset(t)
	Path = filepath.Join(dir, "sub", filename)
	t.Setenv("MANGAL_DOWNLOAD_PATH", "/env/path")
	if err := Write(); err != nil {
		t.Fatal(err)
	}
	file := readConfig(t)
	if strings.Contains(file, "/env/path") {
		t.Errorf("env value written:\n%s", file)
	}
	if !strings.Contains(file, "parallelism = 15") {
		t.Errorf("defaults not written:\n%s", file)
	}
}

func TestMirrors(t *testing.T) {
	reset(t)
	writeConfig(t, "[providers.mirrors]\nmango-mangakakalot = 'https://www.mangakakalove.com/'\n")
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
	if file := readConfig(t); !strings.Contains(file, "mango-manganato = 'https://www.nelomanga.net/'") || strings.Contains(file, "mangakakalove") {
		t.Errorf("want only the manganato mirror:\n%s", file)
	}

	if err := Set(Providers.Mirrors.Key, map[string]any{"mango-x": "not a url"}); err == nil {
		t.Error("invalid mirror: no error")
	}
}

func TestDescriptions(t *testing.T) {
	for _, key := range Keys() {
		if d, _ := Description(key); d == "" {
			t.Errorf("%s has no description", key)
		}
	}
	if !IsSecret(Notification.Discord.WebhookURL.Key) || IsSecret(Download.Path.Key) {
		t.Error("secret keys")
	}
}

func TestKitsuKeys(t *testing.T) {
	reset(t)
	if !Metadata.Kitsu.Enabled.Get() || Read.History.Kitsu.Get() || ReadOptions().SaveKitsu {
		t.Fatal("kitsu defaults: enabled, no sync")
	}
	writeConfig(t, "[read.history]\nkitsu = true\n")
	if err := Load(Path); err != nil {
		t.Fatal(err)
	}
	if !ReadOptions().SaveKitsu {
		t.Error("read.history.kitsu doesn't sync")
	}
	// no provider to sync to
	if err := Metadata.Kitsu.Enabled.Set(false); err != nil {
		t.Fatal(err)
	}
	if ReadOptions().SaveKitsu {
		t.Error("syncs to a disabled kitsu")
	}
}

func TestKindsAndValues(t *testing.T) {
	for key, want := range map[string]Kind{
		Download.Metadata.Strict.Key: KindBool,
		Providers.Parallelism.Key:    KindInt,
		Providers.Mirrors.Key:        KindMap,
		Download.Path.Key:            KindString,
	} {
		if got := KindOf(key); got != want {
			t.Errorf("%s: kind %d, want %d", key, got, want)
		}
	}
	if got := Values(Icons.Key); len(got) != 2 {
		t.Errorf("icons values %v", got)
	}
	if got := Values(Providers.MangaPlus.Quality.Key); len(got) != 3 {
		t.Errorf("quality values %v", got)
	}
	if got := Values(Download.Path.Key); got != nil {
		t.Errorf("path values %v", got)
	}
}

package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestWriteListsCommentedDefaults(t *testing.T) {
	reset(t)
	if err := Write(); err != nil {
		t.Fatal(err)
	}
	first := readConfig(t)
	for _, want := range []string{"[serve]\n", "# web = false\n", "# parallelism = 4\n", "[notification.discord]\n"} {
		if !strings.Contains(first, want) {
			t.Errorf("no %q in:\n%s", want, first)
		}
	}
	file := viper.New()
	file.SetConfigFile(Path)
	if err := file.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	if keys := file.AllKeys(); len(keys) != 0 {
		t.Errorf("defaults written as values: %v", keys)
	}
	// every non-map key is listed
	for _, key := range Keys() {
		if fields[key].isMap() {
			continue
		}
		leaf := key[strings.LastIndexByte(key, '.')+1:]
		if !strings.Contains(first, "# "+leaf+" = ") {
			t.Errorf("%s not listed", key)
		}
	}
	if err := Load(Path); err != nil {
		t.Fatal(err)
	}
	if len(Unsupported()) != 0 || len(Retired()) != 0 {
		t.Errorf("unsupported %v, retired %v", Unsupported(), Retired())
	}
	if err := Write(); err != nil {
		t.Fatal(err)
	}
	if again := readConfig(t); again != first {
		t.Errorf("second write changed the file:\n%s", again)
	}
}

func TestWriteRefreshesCommentedDefaults(t *testing.T) {
	reset(t)
	// an old default, a retired key's comment, and a multi-line string that looks like one
	writeConfig(t, "[providers]\n# parallelism = 9\n\n[library]\n# path = ''\n\n[download]\npath = '''\n# format = 'x'\n'''\n")
	if err := Write(); err != nil {
		t.Fatal(err)
	}
	got := readConfig(t)
	if !strings.Contains(got, "[providers]\n# parallelism = 4\n") || strings.Contains(got, "parallelism = 9") {
		t.Errorf("old default not refreshed:\n%s", got)
	}
	if strings.Contains(got, "[library]") {
		t.Errorf("retired key's comment left:\n%s", got)
	}
	if !strings.Contains(got, "path = '''\n# format = 'x'\n'''\n") {
		t.Errorf("multi-line string changed:\n%s", got)
	}
}

func TestWriteKeyCommentedDefaults(t *testing.T) {
	reset(t)
	writeConfig(t, "[providers]\n# parallelism = 4\nmangadex.language = 'en'\n\n[serve]\nweb = false # web too\n")
	if err := WriteKey(Providers.Parallelism.Key, "8"); err != nil {
		t.Fatal(err)
	}
	if got := readConfig(t); !strings.HasPrefix(got, "[providers]\nparallelism = 8\nmangadex.language = 'en'\n") {
		t.Errorf("commented default not set in place:\n%s", got)
	}
	// back to the default: commented again
	if err := WriteKey(Providers.Parallelism.Key, "4"); err != nil {
		t.Fatal(err)
	}
	if err := WriteKey(Serve.Web.Key, false); err != nil {
		t.Fatal(err)
	}
	got := readConfig(t)
	for _, want := range []string{"[providers]\n# parallelism = 4\n", "# web = false # web too\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
	if Providers.Parallelism.Get() != 4 || Serve.Web.Get() {
		t.Error("values not set")
	}
}

func TestSuggest(t *testing.T) {
	for key, want := range map[string]string{
		"serve.wbe":              "serve.web",
		"Serve.Web":              "serve.web",
		"providers.parallelsm":   "providers.parallelism",
		"watch.coverage_minimum": "watch.coverage_min",
		"nothing.like.it":        "",
	} {
		if got := Suggest(key); got != want {
			t.Errorf("Suggest(%q) = %q, want %q", key, got, want)
		}
	}
	if _, err := Get("serve.wbe"); err == nil || !strings.Contains(err.Error(), `did you mean "serve.web"`) {
		t.Errorf("unknown key error: %v", err)
	}
}

func TestClean(t *testing.T) {
	reset(t)
	original := "# mine\nicons = 'ascii' # the default\nold_key = 1\n\n[serve]\nwbe = true\nport = 7000\n\n[library]\npath = ''\n\n[providers.mirrors]\nmango-x = 'https://x.example/'\n"
	writeConfig(t, original)
	if err := Load(Path); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(Retired(), []string{"library.path"}) || !slices.Equal(Unsupported(), []string{"old_key", "serve.wbe"}) {
		t.Fatalf("retired %v, unsupported %v", Retired(), Unsupported())
	}
	stale, err := StaleKeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(stale.Unknown) != 2 || stale.Unknown[1] != (UnknownKey{Key: "serve.wbe", Suggestion: "serve.web"}) {
		t.Errorf("unknown %v", stale.Unknown)
	}
	if len(stale.Retired) != 1 || stale.Retired[0].Key != "library.path" || stale.Retired[0].Reason == "" {
		t.Errorf("retired %v", stale.Retired)
	}
	if !slices.Equal(stale.Defaults, []string{"icons"}) {
		t.Errorf("defaults %v", stale.Defaults)
	}

	backup, err := Clean(stale)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(backup) != dir || !strings.HasSuffix(backup, ".bak") {
		t.Errorf("backup %q", backup)
	}
	if data, err := os.ReadFile(backup); err != nil || string(data) != original {
		t.Errorf("backup: %v\n%s", err, data)
	}
	want := "# mine\n# icons = 'ascii' # the default\n\n[serve]\nport = 7000\n\n[providers.mirrors]\nmango-x = 'https://x.example/'\n"
	if got := readConfig(t); got != want {
		t.Errorf("cleaned:\n%s\nwant:\n%s", got, want)
	}
	if stale, err := StaleKeys(); err != nil || !stale.Empty() {
		t.Errorf("still stale: %v %v", stale, err)
	}

	// a second clean on the same second doesn't overwrite the first backup
	writeConfig(t, original)
	stale, _ = StaleKeys()
	second, err := Clean(stale)
	if err != nil {
		t.Fatal(err)
	}
	if second == backup {
		t.Errorf("backup overwritten: %s", second)
	}
}

func TestCleanNoFile(t *testing.T) {
	reset(t)
	stale, err := StaleKeys()
	if err != nil || !stale.Empty() {
		t.Errorf("%v %v", stale, err)
	}
}

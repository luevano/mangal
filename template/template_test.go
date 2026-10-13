package template

import (
	"slices"
	"strings"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/config"
)

func TestCeilFloorTakeChapterNumbers(t *testing.T) {
	data := struct{ Number float32 }{10.5}
	got, err := Exec(`{{ ceil .Number }} {{ floor .Number }} {{ ceil 2 }}`, data)
	if err != nil {
		t.Fatal(err)
	}
	if got != "11 10 2" {
		t.Errorf("got %q", got)
	}
	if _, err := Exec(`{{ ceil "x" }}`, nil); err == nil {
		t.Error("ceil of a string: no error")
	}
}

func TestFuncs(t *testing.T) {
	if !slices.IsSortedFunc(Funcs, func(a, b Func) int { return strings.Compare(a.Name, b.Name) }) {
		t.Error("funcs aren't sorted")
	}
	got, err := Exec(`{{ title "one piece" }}|{{ sanitize ".." }}|{{ replaceAll "a-b" "-" "_" }}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "ONE PIECE|_|a_b" {
		t.Errorf("got %q", got)
	}
}

func TestGetConfig(t *testing.T) {
	got, err := Exec(`{{ getConfig "providers.filter.language" }}|{{ getConfig "nope" }}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "en|<no value>" {
		t.Errorf("got %q", got)
	}
	if _, err := Exec(`{{ getConfig "notification.discord.webhook_url" }}`, nil); err == nil {
		t.Error("secret given to the template")
	}
}

func TestConfigValidatesTemplates(t *testing.T) {
	if _, err := config.Parse(config.Download.Chapter.NameTemplate.Key, "{{ .Chapter.Number | nope }}"); err == nil {
		t.Error("unknown function: no error")
	}
	if _, err := config.Parse(config.Download.Chapter.NameTemplate.Key, "{{ .Chapter.Number | ceil }}"); err != nil {
		t.Error(err)
	}
}

func TestFallbackOnExecuteError(t *testing.T) {
	e := config.Download.Provider.NameTemplate
	old := e.Get()
	t.Cleanup(func() { _ = e.Set(old) })
	if err := e.Set("{{ ceil .Name }}"); err != nil {
		t.Fatal(err)
	}
	if got := Provider(libmangal.ProviderInfo{Name: "Manga/Site"}); got != "Manga_Site" {
		t.Errorf("got %q", got)
	}
}

// a template's output is a file name, or the default one is used
func TestNamesAreOneElement(t *testing.T) {
	key := config.Download.Provider.NameTemplate
	defer func() { _ = key.Set(key.Default) }()
	provider := libmangal.ProviderInfo{ID: "p", Name: "Prov"}
	want := defaults().ProviderName(provider)
	for _, text := range []string{"../{{ .Name }}", "a/b", "", "{{ \"\" }}", ".", ".."} {
		if err := key.Set(text); err != nil {
			t.Fatal(err)
		}
		if got := Provider(provider); got != want {
			t.Errorf("%q gave %q, want the default %q", text, got, want)
		}
	}
	if err := key.Set("{{ .Name }} x"); err != nil {
		t.Fatal(err)
	}
	if got := Provider(provider); got != "Prov x" {
		t.Errorf("got %q", got)
	}
}

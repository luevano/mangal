package config

import (
	"context"
	"github.com/charmbracelet/bubbles/key"
	"github.com/luevano/mangal/v5/tui/util"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	mconfig "github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/tui/base"
)

func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "MANGAL_") {
			os.Unsetenv(name)
		}
	}
	os.Exit(m.Run())
}

const file = `# my config
icons = 'ascii' # plain

[download.metadata]
# keep it strict
strict = true

[providers]
parallelism = 15 # default

[providers.mirrors]
mango-a = 'https://a.example/'
`

// newTest loads file and opens the window on key, reloads counts the provider reloads.
func newTest(t *testing.T, key string) (s *state, path string, reloads *int) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "mangal.toml")
	if err := os.WriteFile(path, []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	mconfig.Reset()
	if err := mconfig.Load(path); err != nil {
		t.Fatal(err)
	}
	reloads = new(int)
	s = New(key).(*state)
	s.reload = func() { *reloads++ }
	s.Resize(base.Size{Width: 100, Height: 30})
	if got, _ := s.selected(); got != key {
		t.Fatalf("selected %q, want %q", got, key)
	}
	return s, path, reloads
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func press(s *state, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		s.Update(context.Background(), msg)
	}
}

func TestToggleBoolKeepsComments(t *testing.T) {
	s, path, reloads := newTest(t, "download.metadata.strict")
	press(s, "enter")
	if mconfig.Download.Metadata.Strict.Get() {
		t.Error("not set in memory")
	}
	if got, want := read(t, path), strings.Replace(file, "strict = true", "strict = false", 1); got != want {
		t.Errorf("file:\n%s\nwant:\n%s", got, want)
	}
	if *reloads != 0 {
		t.Error("providers reloaded for a download key")
	}
	if s.err != "" {
		t.Error(s.err)
	}
}

func TestInvalidIntNotWritten(t *testing.T) {
	s, path, _ := newTest(t, "providers.parallelism")
	press(s, "enter")
	if s.mode != editing || !s.Inputting() {
		t.Fatal("no input")
	}
	s.input.SetValue("abc")
	press(s, "enter")
	if s.err == "" || s.mode != editing {
		t.Errorf("no inline error: %q, mode %d", s.err, s.mode)
	}
	s.input.SetValue("300")
	press(s, "enter")
	if !strings.Contains(s.err, "255") {
		t.Errorf("out of range: %q", s.err)
	}
	if read(t, path) != file {
		t.Error("invalid value written")
	}
	if mconfig.Providers.Parallelism.Get() != 15 {
		t.Error("invalid value set")
	}

	s.input.SetValue("4")
	press(s, "enter")
	if s.mode != browsing || s.err != "" {
		t.Fatalf("not saved: %q", s.err)
	}
	if !strings.Contains(read(t, path), "parallelism = 4 # default") {
		t.Errorf("file:\n%s", read(t, path))
	}

	press(s, "enter", "esc")
	if s.mode != browsing {
		t.Error("esc didn't cancel")
	}
}

func TestEnumCycles(t *testing.T) {
	s, path, _ := newTest(t, "icons")
	press(s, "enter")
	if got, _ := mconfig.Get("icons"); got != "nerd" {
		t.Errorf("icons %v", got)
	}
	if !strings.Contains(read(t, path), "icons = 'nerd' # plain") {
		t.Errorf("file:\n%s", read(t, path))
	}
	press(s, " ")
	if got, _ := mconfig.Get("icons"); got != "ascii" {
		t.Errorf("icons %v", got)
	}
}

func TestProviderKeyReloads(t *testing.T) {
	s, _, reloads := newTest(t, "providers.filter.nsfw")
	press(s, "enter")
	if *reloads != 1 {
		t.Errorf("%d reloads", *reloads)
	}
}

func TestReset(t *testing.T) {
	s, path, _ := newTest(t, "download.metadata.strict")
	press(s, "enter")
	if mconfig.Download.Metadata.Strict.Get() {
		t.Fatal("not toggled")
	}
	press(s, "backspace")
	if !mconfig.Download.Metadata.Strict.Get() || !strings.Contains(read(t, path), "strict = true") {
		t.Error("not reset")
	}
}

func TestMirrors(t *testing.T) {
	s, path, reloads := newTest(t, "providers.mirrors")
	press(s, "enter")
	if s.mode != mapping {
		t.Fatal("not mapping")
	}
	press(s, "n")
	s.input.SetValue("mango-b")
	press(s, "enter")
	s.input.SetValue("not a url")
	press(s, "enter")
	if s.err == "" {
		t.Error("invalid url accepted")
	}
	s.input.SetValue("https://b.example/")
	press(s, "enter")
	if s.err != "" || s.maps.inputting {
		t.Fatalf("not added: %q", s.err)
	}
	want := strings.Replace(file, "mango-a = 'https://a.example/'\n", "mango-a = 'https://a.example/'\nmango-b = 'https://b.example/'\n", 1)
	if got := read(t, path); got != want {
		t.Errorf("file:\n%s\nwant:\n%s", got, want)
	}

	// the cursor is on mango-b, remove mango-a: d asks first (MT-B1)
	press(s, "k", "d")
	if !s.confirm.Active() || len(mconfig.Providers.Mirrors.Get()) != 2 {
		t.Fatal("d removed without asking")
	}
	yes := s.Update(context.Background(), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if yes == nil {
		t.Fatal("d didn't confirm")
	}
	s.Update(context.Background(), yes())
	if got := mconfig.Providers.Mirrors.Get(); len(got) != 1 || got["mango-b"] == "" {
		t.Errorf("mirrors %v", got)
	}
	if strings.Contains(read(t, path), "mango-a") {
		t.Error("not removed from the file")
	}
	if *reloads != 2 {
		t.Errorf("%d reloads", *reloads)
	}
	press(s, "esc")
	if s.mode != browsing {
		t.Error("esc didn't close the entries")
	}

	press(s, "backspace")
	if len(mconfig.Providers.Mirrors.Get()) != 1 {
		t.Error("reset without confirmation")
	}
	press(s, "backspace")
	if len(mconfig.Providers.Mirrors.Get()) != 0 || strings.Contains(read(t, path), "mango-b") {
		t.Error("not reset")
	}
}

func TestSecretMasked(t *testing.T) {
	s, _, _ := newTest(t, "notification.discord.webhook_url")
	if err := mconfig.Set("notification.discord.webhook_url", "https://discord.com/api/webhooks/1/tok"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(item{key: "notification.discord.webhook_url"}.Title(), "tok") {
		t.Error("secret shown")
	}
	press(s, "enter")
	if s.input.EchoMode != textinput.EchoPassword || strings.Contains(s.input.View(), "tok") {
		t.Error("secret not masked while editing")
	}
}

func TestEveryKeyListed(t *testing.T) {
	s, _, _ := newTest(t, "icons")
	s.Filter("")
	if got, want := len(s.list.Items()), len(mconfig.Keys()); got != want {
		t.Errorf("%d items, %d keys", got, want)
	}
	first, _ := s.list.Items()[0].(item)
	if !strings.HasPrefix(first.key, "download.") {
		t.Errorf("first key %q", first.key)
	}
}

// visible are the rows: "section:name" for headers, the key otherwise.
func visible(s *state) []string {
	var rows []string
	for _, row := range s.list.VisibleItems() {
		rows = append(rows, rowID(row))
	}
	return rows
}

func TestSectionsExpand(t *testing.T) {
	s, _, _ := newTest(t, "")
	rows := visible(s)
	for _, row := range rows {
		if !strings.HasPrefix(row, "section:") {
			t.Fatalf("collapsed sections show %q", row)
		}
	}
	if rows[0] != "section:download" {
		t.Fatalf("first section %q", rows[0])
	}

	// enter on a header expands it in place, again collapses it
	press(s, "enter")
	rows = visible(s)
	if rows[0] != "section:download" || rows[1] != "download.chapter.name_template" || rows[len(rows)-1] == "section:download" || !strings.HasPrefix(rows[len(rows)-1], "section:") {
		t.Errorf("expanded download: %v", rows)
	}
	press(s, "enter")
	if got := len(visible(s)); got != len(sectionItems(mconfig.Keys())) {
		t.Errorf("collapsed again: %d rows", got)
	}

	// E expands every section, again collapses them all
	press(s, "E")
	if got := len(visible(s)); got != len(sectionItems(mconfig.Keys()))+len(mconfig.Keys()) {
		t.Errorf("expanded all: %d rows", got)
	}
	press(s, "E")
	if got := len(visible(s)); got != len(sectionItems(mconfig.Keys())) {
		t.Errorf("collapsed all: %d rows", got)
	}
}

func TestSearchCollapsedSections(t *testing.T) {
	s, _, _ := newTest(t, "")
	// / lists every key, the collapsed sections' too (the list filters them as typed)
	press(s, "/")
	if !s.searching || len(s.list.Items()) != len(mconfig.Keys()) {
		t.Fatalf("searching %t, %d rows", s.searching, len(s.list.Items()))
	}
	press(s, "esc")
	s.Filter("parallelism")
	if rows := visible(s); len(rows) == 0 || rows[0] != "providers.parallelism" {
		t.Fatalf("search: %v", rows)
	}
	// esc ends the search, the sections as they were (all collapsed)
	press(s, "esc")
	if s.searching {
		t.Error("still searching")
	}
	for _, row := range visible(s) {
		if !strings.HasPrefix(row, "section:") {
			t.Fatalf("after the search %q shows", row)
		}
	}
}

// TestKeys: no key means two things on the config window (its keys, the list's, the global
// ones; the entries editor and the inputs are their own modes).
func TestKeys(t *testing.T) {
	s, _, _ := newTest(t, "download.path")
	k := s.keys
	if keys := base.KeyCollisions(util.KeyMap{Short: []key.Binding{k.change, k.reset, k.expandAll}}, s.list.KeyMap.Browsing()...); len(keys) != 0 {
		t.Error(keys)
	}
	entries := util.KeyMap{Short: []key.Binding{k.add, k.editEntry, k.remove, k.up, k.down, k.close}}
	if keys := append(util.Collisions(util.Bindings(entries)...), base.KeyInconsistencies(entries)...); len(keys) != 0 {
		t.Errorf("entries editor: %v", keys)
	}
}

// a secret saved to a config file others can read warns, as config set does
func TestSecretReadableWarns(t *testing.T) {
	s, _, _ := newTest(t, "notification.discord.webhook_url")
	press(s, "enter")
	s.input.SetValue("https://discord.com/api/webhooks/1/tok")
	cmd := s.Update(context.Background(), tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("not saved")
	}
	msg, ok := cmd().(base.NotificationWithDurationMsg)
	if !ok || !strings.Contains(msg.Message, "chmod 600") {
		t.Errorf("no warning: %#v", cmd())
	}
}

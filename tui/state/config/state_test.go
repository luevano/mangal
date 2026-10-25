package config

import (
	"context"
	"github.com/luevano/libmangal"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	"github.com/luevano/mangal/v5/tui/util"

	tea "charm.land/bubbletea/v2"
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

[tui]
# keep it skipped
skip_home = true
`

// stored are the database keys the tests start with
var stored = map[string]string{
	"providers.parallelism": "15",
	"providers.mirrors":     `{"mango-a":"https://a.example/"}`,
}

// memStore is the data database's settings in memory.
type memStore struct {
	values map[string]string
	rev    int64
}

func (m *memStore) All() (map[string]string, int64, error) { return maps.Clone(m.values), m.rev, nil }
func (m *memStore) Revision() (int64, error)               { return m.rev, nil }
func (m *memStore) Imported() (bool, error)                { return true, nil }
func (m *memStore) Set(key string, value *string, _ bool, _ string) error {
	m.rev++
	if value == nil {
		delete(m.values, key)
	} else {
		m.values[key] = *value
	}
	return nil
}

// newTest loads file and opens the window on key, reloads counts the provider reloads.
func newTest(t *testing.T, key string) (s *state, path string, reloads *int) {
	s, path, reloads, _ = newTestDB(t, key)
	return s, path, reloads
}

// newTestDB is newTest with the database keys' store.
func newTestDB(t *testing.T, key string) (s *state, path string, reloads *int, db *memStore) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "mangal.toml")
	if err := os.WriteFile(path, []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	mconfig.Reset()
	if err := mconfig.Load(path); err != nil {
		t.Fatal(err)
	}
	db = &memStore{values: maps.Clone(stored)}
	if err := mconfig.UseStore(db); err != nil {
		t.Fatal(err)
	}
	reloads = new(int)
	s = New(key).(*state)
	s.reload = func() { *reloads++ }
	s.Resize(base.Size{Width: 100, Height: 30})
	if got, _ := s.selected(); got != key {
		t.Fatalf("selected %q, want %q", got, key)
	}
	return s, path, reloads, db
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
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = util.KeyPress("enter")
		case "esc":
			msg = util.KeyPress("esc")
		case "backspace":
			msg = util.KeyPress("backspace")
		default:
			msg = util.KeyPress(k)
		}
		s.Update(context.Background(), msg)
	}
}

func TestToggleBoolKeepsComments(t *testing.T) {
	s, path, reloads := newTest(t, "tui.skip_home")
	press(s, "enter")
	if mconfig.TUI.SkipHome.Get() {
		t.Error("not set in memory")
	}
	if got, want := read(t, path), strings.Replace(file, "skip_home = true", "skip_home = false", 1); got != want {
		t.Errorf("file:\n%s\nwant:\n%s", got, want)
	}
	if *reloads != 0 {
		t.Error("providers reloaded for a tui key")
	}
	if s.err != "" {
		t.Error(s.err)
	}
}

func TestInvalidIntNotWritten(t *testing.T) {
	s, path, _, db := newTestDB(t, "providers.parallelism")
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
	if read(t, path) != file || db.values["providers.parallelism"] != "15" {
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
	// the default isn't stored: the key follows it
	if v, ok := db.values["providers.parallelism"]; ok || read(t, path) != file {
		t.Errorf("stored %q, file:\n%s", v, read(t, path))
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
	s, path, _ := newTest(t, "tui.skip_home")
	press(s, "enter")
	if mconfig.TUI.SkipHome.Get() {
		t.Fatal("not toggled")
	}
	press(s, "backspace")
	if !mconfig.TUI.SkipHome.Get() || strings.Contains(read(t, path), "skip_home = false") {
		t.Error("not reset")
	}
}

// TestMirrors: a provider's website is its own setting in its section; the old table's
// row counts until it's written, then goes.
func TestMirrors(t *testing.T) {
	_, path, reloads, db := newTestDB(t, "icons")
	mconfig.RegisterProviders([]libmangal.ProviderInfo{{ID: "mango-a", Name: "A", Website: "https://a.example/", Mirrors: []string{"https://b.example/", "https://c.example/"}}})
	db.values["providers.mirrors"] = `{"mango-a":"https://b.example/"}`
	if err := mconfig.UseStore(db); err != nil {
		t.Fatal(err)
	}
	if got, _ := mconfig.Get("providers.mango-a.mirror"); got != "https://b.example/" {
		t.Fatalf("from the old table: %v", got)
	}
	s := New("providers.mango-a.mirror").(*state)
	s.reload = func() { *reloads++ }
	s.Resize(base.Size{Width: 100, Height: 30})
	if got, _ := s.selected(); got != "providers.mango-a.mirror" {
		t.Fatalf("selected %q", got)
	}
	press(s, "enter")
	if got, want := db.values["providers.mango-a.mirror"], `"https://c.example/"`; got != want || read(t, path) != file {
		t.Errorf("stored %s, want %s", got, want)
	}
	if _, ok := db.values["providers.mirrors"]; ok || mconfig.Providers.Mirrors.Get()["mango-a"] != "https://c.example/" {
		t.Errorf("the table %v, stored %v", mconfig.Providers.Mirrors.Get(), db.values)
	}
	if *reloads != 1 {
		t.Errorf("%d reloads", *reloads)
	}
	// the website itself is the default: nothing stored
	press(s, "enter")
	if _, ok := db.values["providers.mango-a.mirror"]; ok || len(mconfig.Providers.Mirrors.Get()) != 0 {
		t.Errorf("not back to the website: %v", db.values)
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
	if !s.input.Secret() || strings.Contains(s.input.View(), "tok") {
		t.Error("secret not masked while editing")
	}
}

func TestEveryKeyListed(t *testing.T) {
	s, _, _ := newTest(t, "icons")
	s.Filter("")
	if got, want := len(s.list.Items()), len(mconfig.ListedKeys()); got != want {
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
	if got := len(visible(s)); got != len(sectionItems(mconfig.ListedKeys())) {
		t.Errorf("collapsed again: %d rows", got)
	}

	// E expands every section, again collapses them all
	press(s, "E")
	if got := len(visible(s)); got != len(sectionItems(mconfig.ListedKeys()))+len(mconfig.ListedKeys()) {
		t.Errorf("expanded all: %d rows", got)
	}
	press(s, "E")
	if got := len(visible(s)); got != len(sectionItems(mconfig.ListedKeys())) {
		t.Errorf("collapsed all: %d rows", got)
	}
}

func TestSearchCollapsedSections(t *testing.T) {
	s, _, _ := newTest(t, "")
	// / lists every key, the collapsed sections' too (the list filters them as typed)
	press(s, "/")
	if !s.searching || len(s.list.Items()) != len(mconfig.ListedKeys()) {
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

// a secret is saved in the database: the config file's mode doesn't matter (config set
// warns only for a secret in the file)
func TestSecretInDatabaseNoFileWarning(t *testing.T) {
	s, _, _, db := newTestDB(t, "notification.discord.webhook_url")
	press(s, "enter")
	s.input.SetValue("https://discord.com/api/webhooks/1/tok")
	cmd := s.Update(context.Background(), util.KeyPress("enter"))
	if cmd == nil {
		t.Fatal("not saved")
	}
	msg, ok := cmd().(base.NotificationMsg)
	if !ok || msg.Warning || strings.Contains(msg.Message, "chmod") || db.values["notification.discord.webhook_url"] == "" {
		t.Errorf("notice %#v, stored %q", cmd(), db.values["notification.discord.webhook_url"])
	}
}

func TestEverySectionListed(t *testing.T) {
	mconfig.Reset()
	for _, s := range sectionItems(mconfig.ListedKeys()) {
		if !slices.Contains(sections, s.name) {
			t.Errorf("section %q missing from sections", s.name)
		}
		if summaries[s.name] == "" {
			t.Errorf("section %q has no summary", s.name)
		}
	}
	for _, name := range sections {
		if summaries[name] == "" {
			t.Errorf("section %q has no summary", name)
		}
	}
}

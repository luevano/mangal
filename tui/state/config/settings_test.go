package config

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/luevano/libmangal"
	mconfig "github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/util"

	tea "charm.land/bubbletea/v2"
)

func TestStartupKeyNeedsRestart(t *testing.T) {
	s, _, _ := newTest(t, "log.compress")
	if len(mconfig.RestartNeeded()) != 0 {
		t.Fatalf("restart needed before a change: %v", mconfig.RestartNeeded())
	}
	press(s, "enter")
	if got := mconfig.RestartNeeded(); !slices.Equal(got, []string{"log.compress"}) {
		t.Errorf("restart needed %v", got)
	}
	if !strings.Contains(s.View(), "restart it to use the new value") {
		t.Errorf("no notice:\n%s", s.View())
	}
	press(s, "enter")
	if got := mconfig.RestartNeeded(); len(got) != 0 {
		t.Errorf("back to the start value, restart needed %v", got)
	}
}

func TestProviderSection(t *testing.T) {
	newTest(t, "tui.skip_home")
	mconfig.RegisterProviders([]libmangal.ProviderInfo{{ID: "mango-x", Name: "X", Settings: []libmangal.ProviderSetting{
		{Key: "quality", Name: "Quality", Kind: libmangal.SettingString, Default: "high"},
	}}})
	var names []string
	for _, section := range sectionItems(mconfig.ListedKeys()) {
		names = append(names, section.name)
	}
	at := slices.Index(names, "providers.mango-x")
	if at < 0 || names[at-1] != "providers" || slices.Index(names, "providers") != at-1 {
		t.Errorf("sections %v", names)
	}
}

// fakeConfig is the settings' history in memory.
type fakeConfig struct {
	core.Config
	changes []core.ConfigChange
	undone  []int64
}

func (f *fakeConfig) History(_ context.Context, key string, _ int) ([]core.ConfigChange, error) {
	var out []core.ConfigChange
	for _, c := range f.changes {
		if key == "" || c.Key == key {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeConfig) Undo(_ context.Context, id int64) (core.ConfigEntry, error) {
	f.undone = append(f.undone, id)
	if err := mconfig.WriteKeyBy("providers.parallelism", 15, "test"); err != nil {
		return core.ConfigEntry{}, err
	}
	return core.ConfigEntry{Key: "providers.parallelism", Value: 15}, nil
}

func TestHistoryUndo(t *testing.T) {
	s, _, reloads, db := newTestDB(t, "providers.parallelism")
	fake := &fakeConfig{changes: []core.ConfigChange{
		{ID: 2, Key: "providers.parallelism", Old: new("15"), New: new("4"), Who: "tui", At: time.Now()},
		{ID: 1, Key: "download.format", New: new(`"cbz"`), Who: "mangal config set", At: time.Now()},
	}}
	s.cfg = fake
	settle := func(k string) {
		for _, m := range base.Drain(s.Update(context.Background(), util.KeyPress(k))) {
			for _, m := range base.Drain(s.Update(context.Background(), m)) {
				s.Update(context.Background(), m)
			}
		}
	}
	settle("alt+h")
	if s.mode != historying || len(s.history.changes) != 1 || s.history.key != "providers.parallelism" {
		t.Fatalf("history of the key: mode %d %+v", s.mode, s.history)
	}
	if !strings.Contains(s.View(), "15 → 4") {
		t.Errorf("view:\n%s", s.View())
	}
	settle("U")
	if !slices.Equal(fake.undone, []int64{2}) || db.values["providers.parallelism"] != "15" || *reloads != 1 {
		t.Errorf("undone %v, stored %q, %d reloads", fake.undone, db.values["providers.parallelism"], *reloads)
	}
	press(s, "esc")
	if s.mode != browsing {
		t.Error("esc didn't close the history")
	}
}

// serverConfig is a server's config in memory.
type serverConfig struct {
	core.Config
	entries map[string]core.ConfigEntry
	sets    []string
}

func (f *serverConfig) List(context.Context) ([]core.ConfigEntry, error) {
	return slices.Collect(maps.Values(f.entries)), nil
}

func (f *serverConfig) Set(_ context.Context, key string, value any) (core.ConfigEntry, error) {
	e := f.entries[key]
	e.Value = value
	f.entries[key] = e
	f.sets = append(f.sets, fmt.Sprintf("%s=%v", key, value))
	return e, nil
}

// update gives the window msg and what its cmds bring, as the TUI would.
func update(s *state, msg tea.Msg) {
	for _, m := range base.Drain(s.Update(context.Background(), msg)) {
		if _, ok := m.(serverMsg); ok {
			s.Update(context.Background(), m)
		}
	}
}

func TestRemoteSplit(t *testing.T) {
	newTest(t, "tui.skip_home")
	server := &serverConfig{entries: map[string]core.ConfigEntry{
		"download.skip_if_exists": {Key: "download.skip_if_exists", Value: true, Default: true, Kind: "bool", Side: "server", Stored: "file"},
		"providers.parallelism":   {Key: "providers.parallelism", Value: float64(3), Default: float64(15), Kind: "int", Side: "server", Stored: "db"},
		"tui.skip_home":           {Key: "tui.skip_home", Value: false, Kind: "bool", Side: "client"},
		"log.compress":            {Key: "log.compress", Value: true, Kind: "bool", Side: "both", Stored: "file"},
	}}
	base.CurrentRemote = &base.Remote{URL: "http://nas.lan:6977", Perms: []string{core.ScopeRead}}
	t.Cleanup(func() { base.CurrentRemote = nil })
	s := New("").(*state)
	s.cfg = server
	s.Resize(base.Size{Width: 100, Height: 30})
	for _, m := range base.Drain(s.Init(context.Background())) {
		s.Update(context.Background(), m)
	}
	var keys []string
	for _, i := range s.items() {
		keys = append(keys, i.key)
	}
	if !slices.Contains(keys, "download.skip_if_exists") || !slices.Contains(keys, "tui.skip_home") || slices.Contains(keys, "download.format") {
		t.Fatalf("keys %v", keys)
	}
	if i := s.itemOf("providers.parallelism"); i.displayValue() != "3" || i.isDefault() {
		t.Errorf("parallelism %q", i.displayValue())
	}
	// the local one is this machine's, the server's needs admin
	if i := s.itemOf("tui.skip_home"); i.e != nil || i.displayValue() != "true" {
		t.Errorf("tui.skip_home %+v", i)
	}
	// a both key is listed twice: this mangal's and the server's
	both := map[string]bool{}
	for _, i := range s.items() {
		if i.key == "log.compress" {
			both[i.group] = true
		}
	}
	if !both[thisMangal] || !both["nas.lan:6977"] {
		t.Errorf("log.compress in %v", both)
	}
	s.Filter("download.skip_if_exists")
	update(s, util.KeyPress("enter"))
	if len(server.sets) != 0 || !strings.Contains(s.err, "nas.lan:6977's setting") {
		t.Fatalf("a reader set %v, err %q", server.sets, s.err)
	}
	base.CurrentRemote.Perms = core.Scopes
	s.Filter("download.skip_if_exists")
	update(s, util.KeyPress("enter"))
	if !slices.Equal(server.sets, []string{"download.skip_if_exists=false"}) || s.err != "" {
		t.Fatalf("sets %v, err %q", server.sets, s.err)
	}
	if raw, _ := s.itemOf("download.skip_if_exists").raw(); raw != false {
		t.Errorf("shown %v", raw)
	}
	if raw, _ := mconfig.Get("download.skip_if_exists"); raw != true {
		t.Errorf("this machine's changed to %v", raw)
	}
	// this machine's key is still set here
	s.Filter("tui.skip_home")
	update(s, util.KeyPress("enter"))
	if raw, _ := mconfig.Get("tui.skip_home"); raw != false || len(server.sets) != 1 {
		t.Errorf("tui.skip_home %v, server sets %v", raw, server.sets)
	}
}

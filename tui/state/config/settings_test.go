package config

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/luevano/libmangal"
	mconfig "github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
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
	for _, section := range sectionItems(mconfig.Keys()) {
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
	press(s, "alt+h")
	if s.mode != historying || len(s.history.changes) != 1 || s.history.key != "providers.parallelism" {
		t.Fatalf("history of the key: mode %d %+v", s.mode, s.history)
	}
	if !strings.Contains(s.View(), "15 → 4") {
		t.Errorf("view:\n%s", s.View())
	}
	press(s, "U")
	if !slices.Equal(fake.undone, []int64{2}) || db.values["providers.parallelism"] != "15" || *reloads != 1 {
		t.Errorf("undone %v, stored %q, %d reloads", fake.undone, db.values["providers.parallelism"], *reloads)
	}
	press(s, "esc")
	if s.mode != browsing {
		t.Error("esc didn't close the history")
	}
}

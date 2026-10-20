package providers

import (
	"context"
	"errors"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/util"
)

// TestKeys: no key means two things on the screen (its keys, the list's, the global ones).
func TestKeys(t *testing.T) {
	s := New(nil).(*state)
	m := newMirrors(libmangal.ProviderInfo{ID: "mango-mangapill"})
	for name, keys := range map[string][]string{
		"providers": base.KeyCollisions(s.keyMap(), s.List.KeyMap.Browsing()...),
		"mirrors":   base.KeyCollisions(m.keyMap(), m.List.KeyMap.Browsing()...),
	} {
		if len(keys) != 0 {
			t.Errorf("%s: %v", name, keys)
		}
	}
}

type fakeLoader struct{ info libmangal.ProviderInfo }

func (l fakeLoader) String() string               { return l.info.Name }
func (l fakeLoader) Info() libmangal.ProviderInfo { return l.info }
func (l fakeLoader) Load(context.Context) (libmangal.Provider, error) {
	return nil, errors.New("not loaded")
}

// space ticks providers, enter searches the ticked ones at once
func TestMultiSearch(t *testing.T) {
	ctx := context.Background()
	s := New([]libmangal.ProviderLoader{
		fakeLoader{libmangal.ProviderInfo{ID: "a", Name: "Alpha"}},
		fakeLoader{libmangal.ProviderInfo{ID: "b", Name: "Beta"}},
		fakeLoader{libmangal.ProviderInfo{ID: "c", Name: "Gamma"}},
	}).(*state)
	s.Resize(base.Size{Width: 80, Height: 20})
	s.Update(ctx, util.KeyPress("space"))
	s.Update(ctx, util.KeyPress("down"))
	s.Update(ctx, util.KeyPress("down"))
	s.Update(ctx, util.KeyPress("space"))
	next, ok := s.Update(ctx, util.KeyPress("enter"))().(base.State)
	if !ok || next.Title().Text != "Search" || next.Subtitle() != "on Alpha, Gamma" {
		t.Fatalf("enter: %#v", next)
	}
	if bad := base.KeyInconsistencies(s.keyMap()); len(bad) != 0 {
		t.Errorf("keys %v", bad)
	}
}

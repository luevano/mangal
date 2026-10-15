package base

import (
	"slices"
	"testing"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"github.com/luevano/mangal/v5/tui/util"
)

// TestKeysFree: no Key* key is a global or list key (as tui/model/list has them, the
// bubbles list's without b u f d), so each works on every screen.
func TestKeysFree(t *testing.T) {
	l := list.DefaultKeyMap()
	taken := append(GlobalKeys(), newKeyMap().quitQ,
		l.CursorUp, l.CursorDown, l.PrevPage, l.NextPage, l.GoToStart, l.GoToEnd, l.Filter, l.ClearFilter,
		util.Bind("reverse", "R"))
	old, newer := Keys()
	for _, b := range append(old, newer...) {
		for _, k := range screenKeys(b) {
			if slices.Contains([]string{"b", "u", "f", "d"}, k) || b.Help().Desc == "up" || b.Help().Desc == "down" {
				continue
			}
			if i := slices.IndexFunc(taken, func(t key.Binding) bool { return slices.Contains(t.Keys(), k) }); i >= 0 {
				t.Errorf("%s: %s is %s's", b.Help().Desc, k, taken[i].Help().Desc)
			}
		}
	}
}

// TestOneKeyOneMeaning: an action has one key (no l/L pairs), a key one meaning (the
// keyConcepts' helps when it has more).
func TestOneKeyOneMeaning(t *testing.T) {
	old, newer := Keys()
	byKey, byDesc := map[string][]string{}, map[string][]string{}
	for _, b := range append(old, newer...) {
		keys, desc := screenKeys(b), b.Help().Desc
		if len(keys) != 1 {
			t.Errorf("%s: keys %q, want one", desc, keys)
		}
		for _, k := range keys {
			if !slices.Contains(byKey[k], desc) {
				byKey[k] = append(byKey[k], desc)
			}
			if !slices.Contains(byDesc[desc], k) {
				byDesc[desc] = append(byDesc[desc], k)
			}
		}
	}
	for desc, keys := range byDesc {
		if len(keys) > 1 {
			t.Errorf("%s: keys %q, want one", desc, keys)
		}
	}
	for k, descs := range byKey {
		concept, ok := keyConcepts[k]
		switch {
		case len(descs) > 1 && !ok:
			t.Errorf("%q means %q", k, descs)
		case ok:
			for _, d := range descs {
				if !slices.Contains(concept, d) {
					t.Errorf("%q: %q isn't in its concept %q", k, d, concept)
				}
			}
		}
	}
	for k, concept := range keyConcepts {
		for _, d := range concept {
			if !slices.Contains(byKey[k], d) {
				t.Errorf("keyConcepts %q: no key has %q", k, d)
			}
		}
	}
}

func TestKeyInconsistencies(t *testing.T) {
	screen := util.KeyMap{Short: []key.Binding{
		util.Bind("library manga", "l"),
		util.BindNamedKey("enter/e", "edit", "enter", "e"),
		util.Bind("merge", "m"),
		util.Bind("squash", "m"),
		util.Bind("frobnicate", "Q"),
		util.Bind("open", "enter"),
	}}
	got := KeyInconsistencies(screen)
	want := []string{"library manga: l, not L", "merge: m, not J", "m: squash, is metadata", "frobnicate: Q isn't a base key"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

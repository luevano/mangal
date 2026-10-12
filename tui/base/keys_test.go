package base

import (
	"slices"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/luevano/mangal/v5/tui/util"
)

// TestSharedKeys: the newer shared keys are free on every screen, no global or list key
// (as the list has them before a screen reserves any) uses them. The viewport's s (change
// it) is only for the viewport.
func TestSharedKeys(t *testing.T) {
	l := list.DefaultKeyMap()
	taken := append(GlobalKeys(), newKeyMap().quitQ,
		l.CursorUp, l.CursorDown, l.PrevPage, l.NextPage, l.GoToStart, l.GoToEnd, l.Filter, l.ClearFilter,
		util.Bind("reverse", "R"))
	_, newer := sharedKeys()
	for _, b := range newer {
		for _, k := range b.Keys() {
			if i := slices.IndexFunc(taken, func(t key.Binding) bool { return slices.Contains(t.Keys(), k) }); i >= 0 {
				t.Errorf("%s: %s is %s's", b.Help().Desc, k, taken[i].Help().Desc)
			}
		}
	}
}

func TestKeyInconsistencies(t *testing.T) {
	screen := util.KeyMap{Short: []key.Binding{
		util.Bind("library manga", "l"),
		util.BindNamedKey("enter/e", "edit", "enter", "e"),
		util.Bind("open", "L"),
	}}
	got := KeyInconsistencies(screen)
	if want := []string{"library manga: l, not L"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

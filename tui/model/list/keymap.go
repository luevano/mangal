package list

import (
	"cmp"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/luevano/mangal/v5/tui/util"
)

var _ help.KeyMap = (*KeyMap)(nil)

func newKeyMap(listKeyMap *list.KeyMap) KeyMap {
	return KeyMap{
		List:    listKeyMap,
		Reverse: util.Bind("reverse", "R"),
	}
}

// KeyMap implements help.KeyMap.
type KeyMap struct {
	List *list.KeyMap

	Reverse key.Binding
}

// ShortHelp implements help.KeyMap.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.List.Filter,
		k.Reverse,
		k.List.CursorUp,
		k.List.CursorDown,
	}
}

// FullHelp implements help.KeyMap.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		k.ShortHelp(),
		{
			k.List.NextPage,
			k.List.PrevPage,
			k.List.GoToStart,
			k.List.GoToEnd,
		},
		{
			k.List.ClearFilter,
			k.List.CancelWhileFiltering,
			k.List.AcceptWhileFiltering,
		},
	}
}

// Browsing are the list's keys outside of filtering: moving, paging, filtering and
// reversing.
func (k KeyMap) Browsing() []key.Binding {
	l := k.List
	return []key.Binding{l.CursorUp, l.CursorDown, l.PrevPage, l.NextPage, l.GoToStart, l.GoToEnd, l.Filter, k.Reverse}
}

// keyNames are the help names of the special keys.
var keyNames = map[string]string{"up": "↑", "down": "↓", "left": "←", "right": "→", "pgdown": "pgdn"}

// Reserve takes the keys from the list's own bindings, for screen keys: the help shows the
// keys left.
func (m *Model) Reserve(keys ...string) {
	l := &m.Model.KeyMap
	for _, b := range []*key.Binding{&l.CursorUp, &l.CursorDown, &l.PrevPage, &l.NextPage, &l.GoToStart, &l.GoToEnd, &l.Filter} {
		left := slices.DeleteFunc(slices.Clone(b.Keys()), func(k string) bool { return slices.Contains(keys, k) })
		if len(left) == len(b.Keys()) {
			continue
		}
		names := make([]string, 0, 3)
		for _, k := range left[:min(3, len(left))] {
			names = append(names, cmp.Or(keyNames[k], k))
		}
		b.SetKeys(left...)
		b.SetHelp(strings.Join(names, "/"), b.Help().Desc)
	}
}

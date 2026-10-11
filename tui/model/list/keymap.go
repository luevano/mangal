package list

import (
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

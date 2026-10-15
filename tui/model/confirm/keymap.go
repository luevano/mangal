package confirm

import (
	"slices"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"github.com/luevano/mangal/v5/tui/util"
)

var _ help.KeyMap = (*keyMap)(nil)

func newKeyMap() keyMap {
	return keyMap{
		yes: util.Bind("yes", "y", "enter"),
		no:  util.Bind("no", "n", "esc"),
	}
}

// keyMap implements help.keyMap.
type keyMap struct {
	yes,
	no key.Binding
	// choices replace yes, see Dialog.Choose
	choices []key.Binding
	// scroll is shown while the message overflows
	scroll    key.Binding
	scrolling bool
}

// ShortHelp implements help.keyMap.
func (k keyMap) ShortHelp() []key.Binding {
	keys := []key.Binding{k.yes, k.no}
	if len(k.choices) != 0 {
		keys = append(slices.Clone(k.choices), k.no)
	}
	if k.scrolling {
		keys = append(keys, k.scroll)
	}
	return keys
}

// FullHelp implements help.keyMap.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		k.ShortHelp(),
	}
}

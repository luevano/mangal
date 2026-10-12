package confirm

import (
	"slices"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
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
}

// ShortHelp implements help.keyMap.
func (k keyMap) ShortHelp() []key.Binding {
	if len(k.choices) != 0 {
		return append(slices.Clone(k.choices), k.no)
	}
	return []key.Binding{
		k.yes,
		k.no,
	}
}

// FullHelp implements help.keyMap.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		k.ShortHelp(),
	}
}

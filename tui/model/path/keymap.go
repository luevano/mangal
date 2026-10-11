package path

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/luevano/mangal/v5/tui/util"
)

var _ help.KeyMap = (*KeyMap)(nil)

func newKeyMap() KeyMap {
	return KeyMap{
		Copy: util.Bind("copy", "c", "enter"),
		quit: util.Bind("quit", "q", "ctrl+c"),
	}
}

// KeyMap implements help.KeyMap.
type KeyMap struct {
	Copy,
	quit key.Binding
}

// ShortHelp implements help.KeyMap.
func (k *KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Copy,
		k.quit,
	}
}

// FullHelp implements help.KeyMap.
func (k *KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}

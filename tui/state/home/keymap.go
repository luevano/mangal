package home

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/luevano/mangal/v5/tui/util"
)

var _ help.KeyMap = (*keyMap)(nil)

func newKeyMap() keyMap {
	return keyMap{
		confirm:  util.Bind("providers", "enter"),
		library:  util.Bind("library", "l"),
		accounts: util.Bind("accounts", "a"),
		migrate:  util.Bind("migrate", "M"),
	}
}

// keyMap implements help.keyMap.
type keyMap struct {
	confirm,
	library,
	accounts,
	migrate key.Binding
}

// ShortHelp implements help.KeyMap.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.confirm,
		k.library,
		k.accounts,
		k.migrate,
	}
}

// FullHelp implements help.KeyMap.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		k.ShortHelp(),
	}
}

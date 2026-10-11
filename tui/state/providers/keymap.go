package providers

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/luevano/mangal/v5/tui/util"
)

var _ help.KeyMap = (*keyMap)(nil)

func newKeyMap() keyMap {
	return keyMap{
		confirm:  util.Bind("confirm", "enter"),
		info:     util.Bind("info", "i"),
		closeAll: util.Bind("close all", "backspace"),
	}
}

// keyMap implements help.keyMap.
type keyMap struct {
	confirm,
	info,
	closeAll key.Binding
}

// ShortHelp implements help.KeyMap.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.confirm,
		k.info,
	}
}

// FullHelp implements help.KeyMap.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		append(k.ShortHelp(), k.closeAll),
	}
}

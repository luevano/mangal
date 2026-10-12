package format

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/util"
)

var _ help.KeyMap = (*keyMap)(nil)

func newKeyMap() keyMap {
	return keyMap{
		setRead:     base.KeyRead,
		setDownload: base.KeyDownload,
		setBoth:     util.Bind("both", "enter"),
		back:        util.Bind("back", "esc"),
	}
}

// keyMap implements help.keyMap.
type keyMap struct {
	setRead,
	setDownload,
	setBoth,
	back key.Binding
}

// ShortHelp implements help.KeyMap.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.setRead,
		k.setDownload,
	}
}

// FullHelp implements help.KeyMap.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		k.ShortHelp(),
		{
			k.setBoth,
			k.back,
		},
	}
}

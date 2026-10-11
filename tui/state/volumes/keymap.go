package volumes

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/luevano/mangal/v5/tui/util"
)

var _ help.KeyMap = (*keyMap)(nil)

func newKeyMap() keyMap {
	return keyMap{
		confirm:  util.Bind("confirm", "enter"),
		anilist:  util.Bind("anilist", "A"),
		metadata: util.Bind("metadata", "m"),
		info:     util.Bind("info", "i"),
	}
}

// keyMap implements help.keyMap.
type keyMap struct {
	confirm,
	anilist,
	metadata,
	info key.Binding
}

// ShortHelp implements help.keyMap.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.confirm,
		k.anilist,
		k.metadata,
		k.info,
	}
}

// FullHelp implements help.keyMap.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		k.ShortHelp(),
	}
}

package mangas

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/luevano/mangal/v5/tui/util"
)

var _ help.KeyMap = (*keyMap)(nil)

func newKeyMap() keyMap {
	return keyMap{
		confirm:        util.Bind("confirm", "enter"),
		search:         util.Bind("search", "s"),
		anilist:        util.Bind("anilist", "A"),
		metadata:       util.Bind("metadata", "m"),
		info:           util.Bind("info", "i"),
		toggleFullMeta: util.Bind("toggle full meta", "M"),
	}
}

// keyMap implements help.keyMap.
type keyMap struct {
	confirm,
	search,
	anilist,
	metadata,
	info,
	toggleFullMeta key.Binding
}

// ShortHelp implements help.keyMap.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.confirm,
		k.search,
		k.anilist,
		k.metadata,
		k.info,
	}
}

// FullHelp implements help.keyMap.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{
			k.confirm,
			k.search,
			k.info,
		},
		{
			k.anilist,
			k.metadata,
			k.toggleFullMeta,
		},
	}
}

package viewport

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/luevano/mangal/v5/tui/util"
)

var _ help.KeyMap = (*KeyMap)(nil)

func newKeyMap(vieportKeyMap *viewport.KeyMap) KeyMap {
	return KeyMap{
		Viewport: vieportKeyMap,
		Copy:     util.Bind("copy content", "c"),
		GoTop:    util.BindNamedKey("g/home", "go to start", "g", "home"),
		GoBottom: util.BindNamedKey("G/end", "go to end", "G", "end"),
		Back:     util.Bind("back", "esc"),
	}
}

// KeyMap implements help.KeyMap.
type KeyMap struct {
	// Viewport is the original viewport KeyMap
	Viewport *viewport.KeyMap

	Copy,
	GoTop,
	GoBottom,
	Back key.Binding
}

// ShortHelp implements help.keyMap.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Copy,
		k.Viewport.Up,
		k.Viewport.Down,
	}
}

// FullHelp implements help.keyMap.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{
			k.Copy,
		},
		{
			k.Viewport.Up,
			k.Viewport.Down,
		},
		{
			k.GoTop,
			k.GoBottom,
		},
		{
			k.Viewport.HalfPageUp,
			k.Viewport.HalfPageDown,
		},
		{
			k.Viewport.PageUp,
			k.Viewport.PageDown,
		},
	}
}

package download

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/luevano/mangal/v5/tui/model/viewport"
	"github.com/luevano/mangal/v5/tui/util"
)

var _ help.KeyMap = (*keyMap)(nil)

func newKeyMap(viewport *viewport.KeyMap) keyMap {
	return keyMap{
		viewport: viewport,
		open:     util.Bind("open directory", "o"),
		retry:    util.Bind("retry", "r"),
	}
}

// keyMap implements help.keyMap.
type keyMap struct {
	viewport *viewport.KeyMap

	open,
	retry key.Binding
}

func (k keyMap) shortHelp() []key.Binding {
	return []key.Binding{
		k.open,
		k.retry,
	}
}

// ShortHelp implements help.keyMap.
func (k keyMap) ShortHelp() []key.Binding {
	return append(k.shortHelp(), k.viewport.ShortHelp()...)
}

// FullHelp implements help.keyMap.
func (k keyMap) FullHelp() [][]key.Binding {
	return append([][]key.Binding{k.shortHelp()}, k.viewport.FullHelp()...)
}

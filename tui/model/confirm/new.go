package confirm

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/tui/model/help"
)

func New(width int, color lipgloss.Color) *Model {
	return &Model{
		width:  width,
		styles: defaultStyles(width, color),
		keyMap: newKeyMap(),
		help:   help.New(),
	}
}

package viewport

import (
	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
)

func New() *Model {
	v := viewport.New()
	b := lipgloss.RoundedBorder()
	s := &Model{
		Model:                v,
		borderHorizontalSize: b.GetLeftSize() + b.GetRightSize(),
		borderVerticalSize:   b.GetTopSize() + b.GetBottomSize(),
		style:                lipgloss.NewStyle().BorderStyle(b),
		KeyMap:               newKeyMap(&v.KeyMap),
	}
	s.updateKeybinds()
	return s
}

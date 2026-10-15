package confirm

import (
	"image/color"

	"github.com/luevano/mangal/v5/tui/model/help"
)

func New(width int, color color.Color) *Model {
	return &Model{
		width:  width,
		styles: defaultStyles(width, color),
		keyMap: newKeyMap(),
		help:   help.New(),
	}
}

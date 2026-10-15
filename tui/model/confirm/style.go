package confirm

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

type styles struct {
	title,
	message,
	more lipgloss.Style
}

func defaultStyles(width int, color color.Color) styles {
	return styles{
		title:   lipgloss.NewStyle().Padding(0, 1).Background(color),
		message: lipgloss.NewStyle().Width(width).MaxWidth(width),
		more:    lipgloss.NewStyle().Faint(true),
	}
}

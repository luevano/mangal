package confirm

import "github.com/charmbracelet/lipgloss"

type styles struct {
	title,
	message,
	more lipgloss.Style
}

func defaultStyles(width int, color lipgloss.Color) styles {
	return styles{
		title:   lipgloss.NewStyle().Padding(0, 1).Background(color),
		message: lipgloss.NewStyle().Width(width).MaxWidth(width),
		more:    lipgloss.NewStyle().Faint(true),
	}
}

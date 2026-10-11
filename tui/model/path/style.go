package path

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/style"
)

type styles struct {
	title,
	notification,
	header,
	cell,
	selected,
	view lipgloss.Style
}

func defaultStyles() styles {
	return styles{
		title: lipgloss.NewStyle().
			Background(color.Accent).
			Foreground(color.Background).
			Padding(0, 1).
			MarginRight(1),
		notification: style.Normal.Warning,
		header:       style.Bold.Accent,
		cell:         style.Normal.Base,
		selected:     style.Normal.Background.Background(color.Accent),
		view:         style.Normal.Base.Margin(0, 2),
	}
}

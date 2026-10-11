package anilist

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/style"
)

type styles struct {
	title,
	notification,
	prompt,
	text,
	field,
	selected,
	view lipgloss.Style
}

func defaultStyles() styles {
	return styles{
		title: lipgloss.NewStyle().
			Background(color.Anilist).
			Foreground(color.Bright).
			Padding(0, 1).
			MarginRight(1),
		notification: style.Normal.Warning,
		prompt:       style.Bold.Base.Foreground(color.Anilist),
		text:         lipgloss.NewStyle(),
		field:        lipgloss.NewStyle().PaddingLeft(2),
		selected:     lipgloss.NewStyle().PaddingLeft(1),
		view:         style.Normal.Base.Margin(0, 2),
	}
}

package kitsu

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/style"
)

type styles struct {
	prompt,
	text,
	field,
	selected lipgloss.Style
}

func defaultStyles() styles {
	return styles{
		prompt:   style.Bold.Base.Foreground(color.Kitsu),
		text:     lipgloss.NewStyle(),
		field:    lipgloss.NewStyle().PaddingLeft(2),
		selected: lipgloss.NewStyle().PaddingLeft(1),
	}
}

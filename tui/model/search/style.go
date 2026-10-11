package search

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/style"
)

type styles struct {
	renderSuggestionBox func(str string, width int) string

	suggestions,
	normalSuggestion,
	matchSuggestion lipgloss.Style
}

func defaultStyles() styles {
	sugs := lipgloss.NewStyle().
		MarginLeft(1). // alings with the search input box
		Border(lipgloss.RoundedBorder()).
		BorderTop(false).
		BorderForeground(color.Secondary)
	return styles{
		renderSuggestionBox: func(str string, width int) string {
			return sugs.Render(lipgloss.NewStyle().Width(width).MaxWidth(width).Render(str))
		},
		normalSuggestion: style.Normal.Secondary,
		matchSuggestion:  style.Normal.Base,
		suggestions:      sugs,
	}
}

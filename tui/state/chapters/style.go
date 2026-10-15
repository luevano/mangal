package chapters

import (
	"charm.land/lipgloss/v2"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/style"
)

type styles struct {
	sep,
	subtitle,
	format,
	formatView lipgloss.Style
}

func defaultStyles() styles {
	return styles{
		sep:      style.Bold.Warning.Padding(0, 1),
		subtitle: style.Normal.Secondary, // matches base without padding
		format:   style.Bold.Warning,
		formatView: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(color.Viewport),
	}
}

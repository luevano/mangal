package path

import (
	"charm.land/lipgloss/v2"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/style"
)

type styles struct {
	header,
	cell,
	selected lipgloss.Style
}

func defaultStyles() styles {
	return styles{
		header:   style.Bold.Accent,
		cell:     style.Normal.Base,
		selected: style.Normal.Background.Background(color.Accent),
	}
}

package style

import (
	"image/color"

	"charm.land/lipgloss/v2"
	mcolor "github.com/luevano/mangal/v5/theme/color"
)

type styles struct {
	Base,
	Accent,
	Secondary,
	Background,
	Success,
	Warning,
	Error,
	Loading,
	Viewport lipgloss.Style
}

var (
	Normal styles = newStyles(lipgloss.NewStyle())
	Bold   styles = newStyles(lipgloss.NewStyle().Bold(true))
	Italic styles = newStyles(lipgloss.NewStyle().Italic(true))
)

func newStyles(base lipgloss.Style) styles {
	return styles{
		Base:       base,
		Accent:     newStyle(base, mcolor.Accent),
		Secondary:  newStyle(base, mcolor.Secondary),
		Background: newStyle(base, mcolor.Background),
		Success:    newStyle(base, mcolor.Success),
		Warning:    newStyle(base, mcolor.Warning),
		Error:      newStyle(base, mcolor.Error),
		Loading:    newStyle(base, mcolor.Loading),
		Viewport:   newStyle(base, mcolor.Viewport),
	}
}

func newStyle(base lipgloss.Style, c color.Color) lipgloss.Style {
	return base.Foreground(c)
}

func FlipGrounds(style lipgloss.Style) lipgloss.Style {
	fg := style.GetForeground()
	bg := style.GetBackground()
	return style.Background(fg).Foreground(bg)
}

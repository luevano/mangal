package style

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/theme/color"
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
		Accent:     newStyle(base, color.Accent),
		Secondary:  newStyle(base, color.Secondary),
		Background: newStyle(base, color.Background),
		Success:    newStyle(base, color.Success),
		Warning:    newStyle(base, color.Warning),
		Error:      newStyle(base, color.Error),
		Loading:    newStyle(base, color.Loading),
		Viewport:   newStyle(base, color.Viewport),
	}
}

func newStyle(base lipgloss.Style, color lipgloss.TerminalColor) lipgloss.Style {
	return base.Foreground(color)
}

func Trim(max int) lipgloss.Style {
	return lipgloss.NewStyle().MaxWidth(max)
}

func FlipGrounds(style lipgloss.Style) lipgloss.Style {
	fg := style.GetForeground()
	bg := style.GetBackground()
	return style.Background(fg).Foreground(bg)
}

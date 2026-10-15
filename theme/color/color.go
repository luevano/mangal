package color

import (
	"image/color"
	"sync/atomic"

	"charm.land/lipgloss/v2"
)

var (
	// Available as styles, too
	Accent     = lipgloss.Color("#EB5E28")
	Secondary  = LightDark(lipgloss.Color("#A49FA5"), lipgloss.Color("#777777"))
	Background = lipgloss.Color("#252422")
	Success    = lipgloss.Color("#7EC699")
	Warning    = lipgloss.Color("#EBCA89")
	Error      = lipgloss.Color("#E05252")
	Loading    = lipgloss.Color("#A49FA5")
	Viewport   = lipgloss.Color("#008080")

	// Available only as colors
	Bright = lipgloss.Color("#FEFEFE")

	// Metadata provider colors
	Provider     = lipgloss.Color("#f26f63")
	Anilist      = lipgloss.Color("#02A9FF") // #1E2630
	MyAnimeList  = lipgloss.Color("#2E51A2")
	Kitsu        = lipgloss.Color("#F75239") // #312631
	MangaUpdates = lipgloss.Color("#F28A2E") // no clear code available
	AnimePlanet  = lipgloss.Color("#A72A2D") // #1C3867 #2E4F83 #F69330 #E65448 #A72A2D // not exact
)

// light is false until SetDark says the terminal's background is light: dark is the
// default when it's unknown, as in lipgloss.
var light atomic.Bool

// SetDark sets whether the terminal's background is dark, for the LightDark colors. The
// TUI sets it from tea.BackgroundColorMsg, the CLI from lipgloss.HasDarkBackground.
func SetDark(dark bool) {
	light.Store(!dark)
}

// IsDark is whether the terminal's background is dark (the default).
func IsDark() bool {
	return !light.Load()
}

// LightDark is a color for each background, picked when it's rendered: the styles are
// package values made before the background is known.
func LightDark(onLight, onDark color.Color) color.Color {
	return adaptive{light: onLight, dark: onDark}
}

type adaptive struct {
	light, dark color.Color
}

// RGBA implements color.Color.
func (a adaptive) RGBA() (r, g, b, alpha uint32) {
	return lipgloss.LightDark(IsDark())(a.light, a.dark).RGBA()
}

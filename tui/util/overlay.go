package util

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const reset = "\x1b[m"

// PlaceOverlay draws fg over bg with its top left corner at (x, y), clamped so fg stays
// inside bg. A fg taller than bg makes the result taller.
func PlaceOverlay(x, y int, fg, bg string) string {
	fgLines, fgWidth := lines(fg)
	bgLines, bgWidth := lines(bg)
	x = max(0, min(x, bgWidth-fgWidth))
	y = max(0, min(y, len(bgLines)-len(fgLines)))
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(bg),
		lipgloss.NewLayer(fg).X(x).Y(y).Z(1),
	).Render()
}

// PlaceCenter draws fg over the center of bg, sized width x height.
func PlaceCenter(width, height int, fg, bg string) string {
	_, fgWidth := lines(fg)
	return PlaceOverlay((width-fgWidth)/2, (height-strings.Count(fg, "\n")-1)/2, fg, bg)
}

// lines splits s into lines, with the width of the widest.
func lines(s string) ([]string, int) {
	ls := strings.Split(s, "\n")
	widest := 0
	for _, l := range ls {
		widest = max(widest, ansi.StringWidth(l))
	}
	return ls, widest
}

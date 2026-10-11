package util

import (
	"strings"

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
	for len(bgLines) < y+len(fgLines) {
		bgLines = append(bgLines, "")
	}

	for i, fgLine := range fgLines {
		bgLine := bgLines[y+i]
		var b strings.Builder

		// a wide grapheme cut in half leaves the cut short, filled with spaces
		left := ansi.Truncate(bgLine, x, "")
		b.WriteString(left)
		if strings.Contains(left, "\x1b") {
			b.WriteString(reset)
		}
		b.WriteString(strings.Repeat(" ", x-ansi.StringWidth(left)))

		b.WriteString(fgLine)
		b.WriteString(strings.Repeat(" ", fgWidth-ansi.StringWidth(fgLine)))

		end := x + fgWidth
		if lineWidth := ansi.StringWidth(bgLine); lineWidth > end {
			right := ansi.TruncateLeft(bgLine, end, "")
			// TruncateLeft keeps a wide grapheme cut in half, drop it too
			for cut := end + 1; ansi.StringWidth(right) > lineWidth-end; cut++ {
				right = ansi.TruncateLeft(bgLine, cut, "")
			}
			if strings.Contains(fgLine, "\x1b") {
				b.WriteString(reset)
			}
			b.WriteString(strings.Repeat(" ", lineWidth-end-ansi.StringWidth(right)))
			b.WriteString(right)
		}
		bgLines[y+i] = b.String()
	}
	return strings.Join(bgLines, "\n")
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

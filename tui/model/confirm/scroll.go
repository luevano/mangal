package confirm

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"github.com/charmbracelet/x/ansi"
)

// wrap breaks the message's lines to width, after a space or a path separator when one is
// close enough; a broken line goes on indented a bit more.
func wrap(message string, width int) []string {
	var lines []string
	for _, line := range strings.Split(message, "\n") {
		indent := strings.Repeat(" ", len(line)-len(strings.TrimLeft(line, " "))+2)
		for ansi.StringWidth(line) > width {
			runes := []rune(line)
			fit := fitting(runes, width)
			cut := fit
			// break after the last space or separator of the line's second half
			for i := fit; i > fit/2; i-- {
				if r := runes[i-1]; r == ' ' || r == '/' || r == '\\' {
					cut = i
					break
				}
			}
			lines = append(lines, strings.TrimRight(string(runes[:cut]), " "))
			line = indent + strings.TrimLeft(string(runes[cut:]), " ")
			if len(indent) >= width/2 {
				indent = ""
			}
		}
		lines = append(lines, line)
	}
	return lines
}

// fitting is how many of the runes fit in width cells (wide ones take two), at least one.
func fitting(runes []rune, width int) int {
	cells := 0
	for i, r := range runes {
		if cells += ansi.StringWidth(string(r)); cells > width {
			return max(1, i)
		}
	}
	return len(runes)
}

// scrollKeys are the keys that scroll an overflowing message: the arrows and pgup/pgdown,
// and j/k unless the dialog's answers use them.
func scrollKeys(answers []key.Binding) (up, down, pageUp, pageDown key.Binding) {
	taken := func(k string) bool {
		return slices.ContainsFunc(answers, func(b key.Binding) bool { return slices.Contains(b.Keys(), k) })
	}
	upKeys, downKeys := []string{"up"}, []string{"down"}
	if !taken("j") && !taken("k") {
		upKeys, downKeys = append(upKeys, "k"), append(downKeys, "j")
	}
	return key.NewBinding(key.WithKeys(upKeys...), key.WithHelp("↑/↓", "scroll")),
		key.NewBinding(key.WithKeys(downKeys...)),
		key.NewBinding(key.WithKeys("pgup")),
		key.NewBinding(key.WithKeys("pgdown"))
}

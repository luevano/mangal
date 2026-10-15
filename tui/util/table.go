package util

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
)

// TableKeys sets a table's keys like the lists': pages with h/l (and the arrows,
// pgup/pgdn), no half pages; b u f d and space are the screens' keys.
func TableKeys(k *table.KeyMap) {
	k.LineUp = BindNamedKey("↑/k", "up", "up", "k")
	k.LineDown = BindNamedKey("↓/j", "down", "down", "j")
	k.PageUp = BindNamedKey("←/h/pgup", "prev page", "left", "h", "pgup")
	k.PageDown = BindNamedKey("→/l/pgdn", "next page", "right", "l", "pgdown")
	k.HalfPageUp = key.NewBinding(key.WithDisabled())
	k.HalfPageDown = key.NewBinding(key.WithDisabled())
	k.GotoTop = BindNamedKey("g/home", "go to start", "g", "home")
	k.GotoBottom = BindNamedKey("G/end", "go to end", "G", "end")
}

// TableBrowsing are a table's keys, to check a screen's against them.
func TableBrowsing(k table.KeyMap) []key.Binding {
	return []key.Binding{k.LineUp, k.LineDown, k.PageUp, k.PageDown, k.HalfPageUp, k.HalfPageDown, k.GotoTop, k.GotoBottom}
}

// FitTable sets a table as wide as its columns (cell is its Cell style): bubbles v2's
// table shows nothing without a width.
func FitTable(t *table.Model, cell lipgloss.Style) {
	width := 0
	for _, c := range t.Columns() {
		width += c.Width + cell.GetHorizontalFrameSize()
	}
	t.SetWidth(width)
}

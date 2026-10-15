package util

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

var namedKeys = map[string]rune{
	"enter":     tea.KeyEnter,
	"esc":       tea.KeyEscape,
	"space":     tea.KeySpace,
	"tab":       tea.KeyTab,
	"backspace": tea.KeyBackspace,
	"delete":    tea.KeyDelete,
	"up":        tea.KeyUp,
	"down":      tea.KeyDown,
	"left":      tea.KeyLeft,
	"right":     tea.KeyRight,
	"home":      tea.KeyHome,
	"end":       tea.KeyEnd,
	"pgup":      tea.KeyPgUp,
	"pgdown":    tea.KeyPgDown,
}

var modifiers = map[string]tea.KeyMod{
	"ctrl":  tea.ModCtrl,
	"alt":   tea.ModAlt,
	"shift": tea.ModShift,
}

// KeyPress is the key press of a keystroke as key bindings name it ("d", "G", "alt+d",
// "ctrl+w", "shift+tab", "space", "enter"), or typed text ("xyz"), for tests and for
// replaying keys.
func KeyPress(keystroke string) tea.KeyPressMsg {
	var mod tea.KeyMod
	parts := strings.Split(keystroke, "+")
	for len(parts) > 1 {
		m, ok := modifiers[parts[0]]
		if !ok {
			break
		}
		mod |= m
		parts = parts[1:]
	}
	name := strings.Join(parts, "+")
	if code, ok := namedKeys[name]; ok {
		k := tea.KeyPressMsg{Code: code, Mod: mod}
		if code == tea.KeySpace && mod == 0 {
			k.Text = " "
		}
		return k
	}
	code, _ := utf8.DecodeRuneInString(name)
	if mod&^tea.ModShift != 0 {
		return tea.KeyPressMsg{Code: code, Mod: mod}
	}
	return tea.KeyPressMsg{Code: code, Text: name, Mod: mod}
}

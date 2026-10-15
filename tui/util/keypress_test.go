package util

import "testing"

func TestKeyPress(t *testing.T) {
	for _, k := range []string{"d", "G", "!", "&", "[", "alt+d", "alt+backspace", "ctrl+w", "ctrl+l", "shift+tab", "space", "enter", "esc", "pgdown", "xyz"} {
		if got := KeyPress(k).String(); got != k {
			t.Errorf("KeyPress(%q).String() = %q", k, got)
		}
	}
}

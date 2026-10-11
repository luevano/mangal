package path

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeKeepsValidNames(t *testing.T) {
	// names valid before must come out the same, existing download folders keep their names
	for _, name := range []string{
		"One Piece",
		"[000001.0] Romance Dawn",
		"Vol. 1.0",
		"...",
		"Kaguya-sama wa Kokurasetai",
		"進撃の巨人",
		"a_b",
		"CON",
	} {
		if got := SanitizeUNIX(name); got != name {
			t.Errorf("SanitizeUNIX(%q) = %q", name, got)
		}
	}
}

func TestSanitize(t *testing.T) {
	tests := []struct {
		name, unix, windows string
	}{
		{"", "_", "_"},
		{".", "_", "_"},
		{"..", "_", "_"},
		{"/..", "_", "_"},
		{"a/b", "a_b", "a_b"},
		{"a//b", "a_b", "a_b"},
		{" _a_ ", "a", "a"},
		{"a\x00b\nc\x7f", "a_b_c", "a_b_c"},
		{"a:b", "a:b", "a_b"},
		{"con", "con", "_con"},
		{"LPT1.txt", "LPT1.txt", "_LPT1.txt"},
		{"Console", "Console", "Console"},
	}
	for _, tt := range tests {
		if got := SanitizeUNIX(tt.name); got != tt.unix {
			t.Errorf("SanitizeUNIX(%q) = %q, want %q", tt.name, got, tt.unix)
		}
		if got := SanitizeWindows(tt.name); got != tt.windows {
			t.Errorf("SanitizeWindows(%q) = %q, want %q", tt.name, got, tt.windows)
		}
	}
}

func TestSanitizeCapsLength(t *testing.T) {
	got := SanitizeUNIX(strings.Repeat("進", 100))
	if len(got) > 240 || !utf8.ValidString(got) {
		t.Errorf("got %d bytes, valid utf8: %v", len(got), utf8.ValidString(got))
	}
	if long := strings.Repeat("a", 240); SanitizeUNIX(long) != long {
		t.Error("a name at the limit changed")
	}
}

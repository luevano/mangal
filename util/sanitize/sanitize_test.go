package sanitize

import (
	"strings"
	"testing"
)

func TestText(t *testing.T) {
	for _, tc := range []struct{ in, text, styled string }{
		{"One Piece", "One Piece", "One Piece"},
		{"line\nnext\ttab", "line\nnext\ttab", "line\nnext\ttab"},
		{"ワンピース 第1話", "ワンピース 第1話", "ワンピース 第1話"},
		// OSC 52: writes the clipboard
		{"title\x1b]52;c;ZWNobyBoaQ==\x07 end", "title end", "title end"},
		{"title\x1b]52;c;ZWNobyBoaQ==\x1b\\ end", "title end", "title end"},
		// an unfinished OSC eats the rest
		{"a\x1b]0;window title", "a", "a"},
		// cursor moves, erase
		{"a\x1b[2J\x1b[1;1Hb\x1b[Kc", "abc", "abc"},
		// colors: dropped as plain text, kept when styled
		{"\x1b[1;38;2;255;0;0mred\x1b[0m", "red", "\x1b[1;38;2;255;0;0mred\x1b[0m"},
		{"\x1b[4:3mcurly\x1b[m", "curly", "\x1b[4:3mcurly\x1b[m"},
		// a private CSI ending in m isn't SGR
		{"\x1b[?1049hx\x1b[>4;2m", "x", "x"},
		// two-byte and charset sequences
		{"a\x1bcb\x1b(Bc\x1b", "abc", "abc"},
		// C0 and C1 controls, invalid bytes
		{"a\rb\x07c\x00d\x7fe", "abcde", "abcde"},
		{"a\u009b31mb\u009dc", "a31mbc", "a31mbc"},
		{"a\x9b31m\xffb", "a31mb", "a31mb"},
	} {
		if got := Text(tc.in); got != tc.text {
			t.Errorf("Text(%q) = %q, want %q", tc.in, got, tc.text)
		}
		if got := Styled(tc.in); got != tc.styled {
			t.Errorf("Styled(%q) = %q, want %q", tc.in, got, tc.styled)
		}
	}
}

func TestSafeDoesntAllocate(t *testing.T) {
	s := strings.Repeat("plain title ワンピース\n", 100)
	if n := testing.AllocsPerRun(10, func() { _ = Text(s) }); n != 0 {
		t.Errorf("%v allocs for a safe string", n)
	}
}

func BenchmarkStyled(b *testing.B) {
	frame := strings.Repeat("\x1b[1;38;5;212mOne Piece\x1b[0m · mangadex · 1100 chapters\n", 40)
	b.SetBytes(int64(len(frame)))
	for range b.N {
		_ = Styled(frame)
	}
}

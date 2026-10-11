package util

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestPlaceOverlay(t *testing.T) {
	tests := []struct {
		name   string
		x, y   int
		fg, bg string
		want   string
	}{
		{
			name: "middle",
			x:    1, y: 1,
			fg:   "ab\ncd",
			bg:   "....\n....\n....\n....",
			want: "....\n.ab.\n.cd.\n....",
		},
		{
			name: "clamped inside",
			x:    10, y: 10,
			fg:   "ab",
			bg:   "....\n....",
			want: "....\n..ab",
		},
		{
			name: "short bg line",
			x:    3, y: 0,
			fg:   "ab",
			bg:   ".\n.....",
			want: ".  ab\n.....",
		},
		{
			name: "uneven fg lines",
			x:    0, y: 0,
			fg:   "abc\nd",
			bg:   "....\n....",
			want: "abc.\nd  .",
		},
		{
			name: "fg taller than bg",
			x:    0, y: 0,
			fg:   "a\nb\nc",
			bg:   "..",
			want: "a.\nb\nc",
		},
		{
			name: "wide grapheme cut on the left",
			x:    1, y: 0,
			fg:   "ab",
			bg:   "日本.",
			want: " ab .",
		},
		{
			name: "wide grapheme cut on the right",
			x:    0, y: 0,
			fg:   "abc",
			bg:   "日本..",
			want: "abc ..",
		},
		{
			name: "emoji kept whole",
			x:    2, y: 0,
			fg:   "x",
			bg:   "👍.👍",
			want: "👍x👍",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ansi.Strip(PlaceOverlay(tt.x, tt.y, tt.fg, tt.bg))
			if got != tt.want {
				t.Errorf("got\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestPlaceOverlayStyles(t *testing.T) {
	red := "\x1b[31m"
	bg := red + "aaaaaa" + reset
	got := PlaceOverlay(2, 0, "\x1b[1mXY"+reset, bg)

	if ansi.Strip(got) != "aaXYaa" {
		t.Fatalf("text: got %q", ansi.Strip(got))
	}
	left, right, ok := strings.Cut(got, "\x1b[1mXY"+reset)
	if !ok {
		t.Fatalf("fg not drawn whole: %q", got)
	}
	// the bg style ends before fg and resumes after it
	if !strings.HasSuffix(left, reset) {
		t.Errorf("left side not reset: %q", left)
	}
	if !strings.Contains(right, red) {
		t.Errorf("right side lost the bg style: %q", right)
	}
	if strings.Count(got, "\x1b") != strings.Count(got, "m") {
		t.Errorf("broken escape sequence: %q", got)
	}
}

func TestPlaceCenter(t *testing.T) {
	bg := ".....\n.....\n....."
	got := PlaceCenter(5, 3, "x", bg)
	if want := ".....\n..x..\n....."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

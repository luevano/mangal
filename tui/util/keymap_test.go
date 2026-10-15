package util

import (
	"slices"
	"testing"

	"charm.land/bubbles/v2/key"
)

func TestCollisions(t *testing.T) {
	gaps, start := Bind("check gaps", "g"), BindNamedKey("g/home", "go to start", "home", "g")
	next := Bind("next page", "right", "l", "d")
	got := Collisions(gaps, start, next, Bind("live log", "t"), Bind("delete", "d"), Bind("delete", "d"))
	if want := []string{"d: next page, delete", "g: check gaps, go to start"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := Bindings(KeyMap{Short: []key.Binding{gaps, gaps, next}}); len(got) != 2 {
		t.Errorf("bindings %d", len(got))
	}
}

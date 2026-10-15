package format

import (
	"github.com/luevano/mangal/v5/tui/util"

	"testing"

	"github.com/luevano/mangal/v5/theme/color"
)

// a filter that matches no format: the keys don't set one (no item to read it from)
func TestNoMatch(t *testing.T) {
	m := New(color.Viewport)
	m.Init()
	m.list.SetFilterText("nothing like a format")
	if m.list.SelectedItem() != nil {
		t.Fatal("a format matches")
	}
	for _, k := range []string{"r", "d"} {
		if cmd := m.Update(util.KeyPress(k)); cmd != nil {
			if _, ok := cmd().(error); ok {
				t.Errorf("%s: an error", k)
			}
		}
	}
	m.Update(util.KeyPress("enter"))
}

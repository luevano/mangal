package list

import (
	"fmt"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	"github.com/luevano/mangal/v5/tui/base"
)

type testItem string

func (i testItem) Title() string       { return string(i) }
func (i testItem) Description() string { return "" }
func (i testItem) FilterValue() string { return string(i) }

// with a filter on, selecting an index of all the items (past the shown ones) must not
// break the render
func TestSelectFiltered(t *testing.T) {
	var names []string
	for n := range 60 {
		names = append(names, fmt.Sprintf("manga %02d", n))
	}
	m := New(2, 1, "manga", "mangas", names, func(s string) list.DefaultItem { return testItem(s) })
	m.Resize(base.Size{Width: 80, Height: 20})
	m.SetFilterText("manga 42")
	if got := len(m.VisibleItems()); got != 1 {
		t.Fatalf("%d shown", got)
	}
	m.Select(42)
	_ = m.View()
	if m.Index() != 0 {
		t.Errorf("index %d", m.Index())
	}

	// SetItems with a filter on: the shown items are filtered again later
	items := m.Items()
	m.SetItems(items)
	m.Select(42)
	_ = m.View()
}

// TestPageKeys: h/l (arrows, pgup/pgdn) page, b u f d are free for the screens.
func TestPageKeys(t *testing.T) {
	m := New(1, 0, "manga", "mangas", []string{"a"}, func(s string) list.DefaultItem { return testItem(s) })
	prev, next := m.KeyMap.List.PrevPage, m.KeyMap.List.NextPage
	if got := fmt.Sprint(prev.Keys(), next.Keys()); got != "[left h pgup] [right l pgdown]" {
		t.Errorf("pages: %s", got)
	}
	if got := prev.Help().Key + " " + next.Help().Key; got != "←/h/pgup →/l/pgdn" {
		t.Errorf("help: %s", got)
	}
}

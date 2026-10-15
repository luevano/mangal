package list

import (
	"github.com/luevano/mangal/v5/tui/util"

	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	"charm.land/lipgloss/v2"
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

type keyedItem struct{ id, title string }

func (i keyedItem) Title() string       { return i.title }
func (i keyedItem) Description() string { return "" }
func (i keyedItem) FilterValue() string { return i.title }
func (i keyedItem) ItemKey() string     { return i.id }

func keyedItems(titles ...string) []list.Item {
	items := make([]list.Item, len(titles))
	for n, t := range titles {
		items[n] = keyedItem{id: t[:1], title: t}
	}
	return items
}

func newKeyed(t *testing.T, titles ...string) *Model {
	t.Helper()
	m := New(1, 0, "item", "items", []string{}, func(s string) list.DefaultItem { return testItem(s) })
	m.Resize(base.Size{Width: 80, Height: 20})
	m.SetItems(keyedItems(titles...))
	return m
}

func selectedKey(m *Model) string {
	if i, ok := m.SelectedItem().(keyedItem); ok {
		return i.id
	}
	return ""
}

func TestSetItemsKeepSelection(t *testing.T) {
	m := newKeyed(t, "a apple", "b banana", "c cherry")
	m.Select(2)
	// a reload: the order changed, an item came before it
	m.SetItemsKeep(keyedItems("d date", "c cherry (new)", "a apple", "b banana"))
	if got := selectedKey(m); got != "c" {
		t.Errorf("selected %q, want c", got)
	}
	// gone: the same place
	m.SetItemsKeep(keyedItems("d date", "a apple", "b banana"))
	if got := m.Index(); got != 1 {
		t.Errorf("index %d, want 1", got)
	}
	// fewer: clamped
	m.SetItemsKeep(keyedItems("d date"))
	if got := selectedKey(m); got != "d" {
		t.Errorf("selected %q, want d", got)
	}
}

func TestSetItemsKeepFilter(t *testing.T) {
	m := newKeyed(t, "a apple", "b banana", "c blueberry", "d date")
	m.SetFilterText("b")
	if n := len(m.VisibleItems()); n != 2 {
		t.Fatalf("%d shown with the filter", n)
	}
	m.Select(1)
	want := selectedKey(m)

	m.SetItemsKeep(keyedItems("e elderberry", "a apple", "b banana", "c blueberry", "d date"))
	if m.FilterState() != list.FilterApplied || m.FilterValue() != "b" {
		t.Fatalf("filter lost: %v %q", m.FilterState(), m.FilterValue())
	}
	if n := len(m.VisibleItems()); n != 3 {
		t.Errorf("%d shown, the new item isn't filtered", n)
	}
	if got := selectedKey(m); got != want {
		t.Errorf("selected %q, want %q", got, want)
	}
	_ = m.View()
}

func TestSetItemsKeepWhileTyping(t *testing.T) {
	m := newKeyed(t, "a apple", "b banana")
	m.SetFilterText("ban")
	m.SetFilterState(list.Filtering)
	m.SetItemsKeep(keyedItems("a apple", "b banana", "c bandana"))
	if !m.Filtering() {
		t.Error("stopped filtering")
	}
	if n := len(m.VisibleItems()); n != 2 {
		t.Errorf("%d shown, want 2", n)
	}
}

func TestSetItemsKeepReversed(t *testing.T) {
	m := newKeyed(t, "a apple", "b banana", "c cherry")
	m.Update(util.KeyPress("R"))
	if got := selectedKey(m); got != "a" {
		t.Errorf("reverse moved the selection to %q", got)
	}
	m.SetItemsKeep(keyedItems("a apple", "b banana", "c cherry", "d date"))
	if first := m.Items()[0].(keyedItem).id; first != "d" {
		t.Errorf("reload lost the reversed order, first %q", first)
	}
	m.SetItems(keyedItems("a apple", "b banana"))
	if first := m.Items()[0].(keyedItem).id; first != "a" {
		t.Errorf("new items kept the reversed order")
	}
}

// an applied filter leaves room in the header for the notifications
func TestStatusFiltered(t *testing.T) {
	m := New(2, 1, "manga", "mangas", []string{"berserk", "claymore"}, func(s string) list.DefaultItem { return testItem(s) })
	m.Resize(base.Size{Width: 120, Height: 20})
	m.SetFilterText("clay")
	status := m.Status()
	if !strings.Contains(status, "clay") || lipgloss.Width(status) > 40 {
		t.Errorf("status %q, %d wide", status, lipgloss.Width(status))
	}
}

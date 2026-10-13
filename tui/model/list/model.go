package list

import (
	"slices"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/tui/base"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

type Model struct {
	list.Model
	delegate *list.DefaultDelegate
	size     base.Size
	KeyMap   KeyMap
	// reversed is R's order, SetItemsKeep keeps it
	reversed bool
}

// Keyed is an item with an identity across reloads (e.g. its id), for SetItemsKeep to
// keep it selected. Items without it are told apart by their FilterValue.
type Keyed interface {
	ItemKey() string
}

func itemKey(i list.Item) string {
	if k, ok := i.(Keyed); ok {
		return k.ItemKey()
	}
	return i.FilterValue()
}

// Unfiltered is a convenience method to check if the list is currently unfiltered.
func (m *Model) Unfiltered() bool {
	return m.FilterState() == list.Unfiltered
}

// Filtering is a convenience method to check if the list is currently filtering.
func (m *Model) Filtering() bool {
	return m.FilterState() == list.Filtering
}

// Subtitle returns the quantified amount of items.
// For example: "1 manga", "10 chapters", "0 pages".
func (m *Model) Subtitle() string {
	singular, plural := m.StatusBarItemName()
	return stringutil.Quantify(len(m.VisibleItems()), singular, plural)
}

func (m *Model) Status() string {
	var p string
	if len(m.Items()) != 0 {
		p = m.Paginator.View()
	}

	if m.FilterState() == list.Filtering || m.FilterValue() != "" {
		return m.Paginator.View() + " " + m.FilterInput.View()
	}

	return p
}

func (m *Model) Size() base.Size {
	return m.size
}

func (m *Model) Resize(size base.Size) tea.Cmd {
	m.size = size
	m.Model.SetSize(m.size.Width, m.size.Height)
	return nil
}

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	if msg, ok := msg.(tea.KeyMsg); ok && m.FilterState() != list.Filtering && key.Matches(msg, m.KeyMap.Reverse) {
		m.reversed = !m.reversed
		items := slices.Clone(m.Items())
		slices.Reverse(items)
		m.replace(items)
		return base.Notify("Reversed")
	}
	list, cmd := m.Model.Update(msg)
	m.Model = list
	return cmd
}

// SetItems replaces the items (new ones, e.g. a search's) and selects the first one.
func (m *Model) SetItems(items []list.Item) tea.Cmd {
	m.ResetSelected()
	m.reversed = false
	cmd := m.Model.SetItems(items)
	m.updateKeybinds()
	return cmd
}

// SetItemsKeep replaces the items with a reload of them (or another view of them): the
// filter (typed or applied) stays and filters the new ones, the R order stays, and the
// cursor stays on the same item (Keyed), else at the same place.
func (m *Model) SetItemsKeep(items []list.Item) tea.Cmd {
	if m.reversed {
		items = slices.Clone(items)
		slices.Reverse(items)
	}
	m.replace(items)
	return nil
}

// replace sets the items, filtered right away, keeping the selected one.
func (m *Model) replace(items []list.Item) {
	selected, index := "", m.Index()
	if i := m.SelectedItem(); i != nil {
		selected = itemKey(i)
	}
	state, filter := m.FilterState(), m.FilterInput.Value()
	m.Model.SetItems(items)
	switch state {
	case list.FilterApplied:
		m.SetFilterText(filter)
	case list.Filtering:
		m.SetFilterText(filter)
		m.SetFilterState(list.Filtering)
	}
	if selected != "" {
		for i, it := range m.VisibleItems() {
			if itemKey(it) == selected {
				index = i
				break
			}
		}
	}
	m.Select(index)
	m.updateKeybinds()
}

// Select selects the item at index of the shown ones, clamped to them: with a filter on,
// an index of all the items can be past the shown ones (or they're still being filtered),
// and a page past them panics on render.
func (m *Model) Select(index int) {
	m.Model.Select(min(max(index, 0), max(len(m.VisibleItems())-1, 0)))
}

// SelectFunc selects the first shown item that match is true for, false if none is.
func (m *Model) SelectFunc(match func(list.Item) bool) bool {
	for i, it := range m.VisibleItems() {
		if match(it) {
			m.Select(i)
			return true
		}
	}
	return false
}

// SetItemHeight sets the height of the delegate, which translates to the items' height.
//
// Clamps to a minimum of 1, in which case the description is hidden.
func (m *Model) SetItemHeight(height int) {
	if height < 2 {
		height = 1
	}
	if height == 1 {
		m.delegate.ShowDescription = false
	} else {
		m.delegate.ShowDescription = true
	}
	m.delegate.SetHeight(height)
	m.SetDelegate(m.delegate)
}

func (m *Model) SetAccentColor(color lipgloss.Color) {
	m.delegate.Styles.SelectedTitle = m.delegate.Styles.SelectedTitle.Foreground(color).BorderForeground(color)
	m.delegate.Styles.SelectedDesc = m.delegate.Styles.SelectedDesc.BorderForeground(color)
	m.SetDelegate(m.delegate)
}

func (m *Model) updateKeybinds() {
	m.KeyMap.Reverse.SetEnabled(len(m.Items()) != 0)
}

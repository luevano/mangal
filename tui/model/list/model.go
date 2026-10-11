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
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.FilterState() == list.Filtering {
			goto end
		}

		switch {
		case key.Matches(msg, m.KeyMap.Reverse):
			slices.Reverse(m.Items())
			return tea.Sequence(
				m.SetItems(m.Items()),
				base.Notify("Reversed"),
			)
		}
	}

end:
	list, cmd := m.Model.Update(msg)
	m.Model = list
	return cmd
}

func (m *Model) SetItems(items []list.Item) tea.Cmd {
	m.ResetSelected()
	return tea.Sequence(
		m.Model.SetItems(items),
		func() tea.Msg {
			m.updateKeybinds()
			return nil
		},
	)
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

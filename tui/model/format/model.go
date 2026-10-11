package format

import (
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
)

type forWhat string

const (
	forRead     forWhat = "read"
	forDownload forWhat = "download"
	forBoth     forWhat = "both"
)

type Model struct {
	list *list.Model

	// pre-rendered
	title,
	help string

	size,
	maxSize base.Size

	keyMap keyMap
}

func (m *Model) Init() tea.Cmd {
	return m.list.Resize(m.size)
}

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.list.FilterState() == _list.Filtering {
			goto end
		}

		// guaranteed to always have items
		i, _ := m.list.SelectedItem().(*item)
		switch {
		case key.Matches(msg, m.keyMap.setRead):
			return m.setFormatForCmd(forRead, i.format)
		case key.Matches(msg, m.keyMap.setDownload):
			return m.setFormatForCmd(forDownload, i.format)
		case key.Matches(msg, m.keyMap.setBoth):
			return m.setFormatForCmd(forBoth, i.format)
		case key.Matches(msg, m.keyMap.back):
			return backCmd
		}
	}
end:
	return m.list.Update(msg)
}

func (m *Model) View() string {
	return lipgloss.JoinVertical(lipgloss.Left, m.title, m.list.View(), " ", m.help)
}

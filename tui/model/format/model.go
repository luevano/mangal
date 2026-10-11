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
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok || m.list.FilterState() == _list.Filtering {
		return m.list.Update(msg)
	}
	// guaranteed to always have items
	i, _ := m.list.SelectedItem().(*item)
	switch {
	case key.Matches(keyMsg, m.keyMap.setRead):
		return m.setFormat(forRead, i.format)
	case key.Matches(keyMsg, m.keyMap.setDownload):
		return m.setFormat(forDownload, i.format)
	case key.Matches(keyMsg, m.keyMap.setBoth):
		return m.setFormat(forBoth, i.format)
	case key.Matches(keyMsg, m.keyMap.back):
		return backCmd
	}
	return m.list.Update(msg)
}

func (m *Model) View() string {
	return lipgloss.JoinVertical(lipgloss.Left, m.title, m.list.View(), " ", m.help)
}

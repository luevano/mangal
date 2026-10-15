package format

import (
	"charm.land/bubbles/v2/key"
	_list "charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

	size base.Size

	keyMap keyMap
}

func (m *Model) Init() tea.Cmd {
	return m.list.Resize(m.size)
}

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok || m.list.FilterState() == _list.Filtering {
		return m.list.Update(msg)
	}
	i, ok := m.list.SelectedItem().(*item)
	if !ok {
		// a filter that matches none
		return m.list.Update(msg)
	}
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

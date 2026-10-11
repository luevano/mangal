package path

import (
	"fmt"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var _ tea.Model = (*Model)(nil)

// Model implements tea.Model.
type Model struct {
	table table.Model
	help  help.Model

	// to be able to clear the screen in standalone
	quitting,
	standalone bool

	// pre-rendered
	title string

	notification         string
	notificationDuration time.Duration

	styles styles
	keyMap KeyMap
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keyMap.quit):
			m.quitting = true
			return m, tea.Quit
		case key.Matches(msg, m.keyMap.Copy):
			row := m.table.SelectedRow()
			path := row[1]

			msg := fmt.Sprintf("copied %q path to clipboard", row[0])
			if err := clipboard.WriteAll(path); err != nil {
				msg = fmt.Sprintf("error copying path to clipboard: %s", err)
			}
			return m, func() tea.Msg {
				return NotificationMsg(msg)
			}
		}
	case NotificationMsg:
		return m, m.notifyCmd(string(msg))
	case NotificationTimeoutMsg:
		m.notification = ""
		return m, nil
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

// View implements tea.Model.
func (m *Model) View() string {
	if !m.standalone {
		return m.table.View()
	}
	if m.quitting {
		return ""
	}
	msg := m.styles.notification.Render(m.notification)
	view := lipgloss.JoinVertical(lipgloss.Left, m.title, msg, m.table.View(), " ", m.help.View(&m.keyMap))
	return m.styles.view.Render(view)
}

func (m *Model) DisableQuitKeybindings() {
	m.keyMap.quit.SetEnabled(false)
}

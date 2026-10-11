package anilist

import (
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/tui/base"
)

// viewStandalone is the standalone rendered, which includes the title,
// the notifications, content and the keybinds.
func (m *Model) viewStandalone(content string, keyMap help.KeyMap) string {
	msg := m.styles.notification.Render(m.notification)
	view := lipgloss.JoinVertical(lipgloss.Left, m.title, msg, content, " ", m.help.View(keyMap))
	return m.styles.view.Render(view)
}

func (m *Model) viewLoggedIn() string {
	view := fmt.Sprintf("Logged in as %s", m.user.String())
	if m.standalone {
		return m.viewStandalone(view, &m.keyMap)
	}
	return view
}

func (m *Model) viewNewLogin() string {
	view := m.viewNewLoginForm()
	if m.standalone {
		return m.viewStandalone(view, &m.keyMap)
	}
	return view
}

func (m *Model) viewLoggedOut() string {
	view := m.viewAvailableLogins()
	if m.standalone {
		return m.viewStandalone(view, base.CombinedKeyMap(&m.list.KeyMap, &m.keyMap))
	}
	return view
}

// viewAvailableLogins renders the list of available logins.
func (m *Model) viewAvailableLogins() string {
	return lipgloss.JoinVertical(lipgloss.Left, "Available logins ("+m.list.Status()+")", " ", m.list.View())
}

// viewNewLoginForm renders the login form.
func (m *Model) viewNewLoginForm() string {
	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.viewField(ID),
		m.viewField(Secret),
		m.viewField(Code),
	)
}

// viewField renders the input fields depending and render hovered accordingly.
func (m *Model) viewField(field Field) string {
	cursor := ""
	render := m.styles.field.Render
	if m.current == field {
		cursor = m.selectCursor
		render = m.styles.selected.Render
	}

	switch field {
	case ID:
		return render(cursor + m.idInput.View())
	case Secret:
		return render(cursor + m.secretInput.View())
	case Code:
		return render(cursor + m.codeInput.View())
	default:
		return "unkown field"
	}
}

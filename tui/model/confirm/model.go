package confirm

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/tui/util"
)

type Model struct {
	title,
	message string

	width int

	// pre-rendered as the content is static
	view string

	styles styles
	keyMap keyMap
	help   help.Model
}

func (m *Model) SetData(title, message string) {
	m.title = title
	m.message = message
	m.preRender()
}

// SetYesKeys replaces the keys that confirm (y and enter by default), the help shows
// them all but enter.
func (m *Model) SetYesKeys(keys ...string) {
	shown := slices.DeleteFunc(slices.Clone(keys), func(k string) bool { return k == "enter" })
	m.keyMap.yes = util.BindNamedKey(strings.Join(shown, "/"), "yes", keys...)
	m.preRender()
}

func (m *Model) preRender() {
	title := m.styles.title.Render(m.title)
	message := m.styles.message.Render(m.message)
	helpStr := lipgloss.PlaceHorizontal(m.width, lipgloss.Center, m.help.View(m.keyMap))
	m.view = lipgloss.JoinVertical(lipgloss.Left, title, " ", message, " ", helpStr)
}

type answer int

const (
	noAnswer answer = iota
	answerYes
	answerNo
)

func (m *Model) answer(msg tea.Msg) answer {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(msg, m.keyMap.yes):
			return answerYes
		case key.Matches(msg, m.keyMap.no):
			return answerNo
		}
	}
	return noAnswer
}

func (m *Model) View() string {
	return m.view
}

package confirm

import (
	"fmt"
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
	// lines are the message wrapped to width, offset the first one shown and height how
	// many fit (0 for all)
	lines          []string
	offset, height int

	// pre-rendered as the content is static
	view string

	styles styles
	keyMap keyMap
	help   help.Model
}

func (m *Model) SetData(title, message string) {
	m.title = title
	m.message = message
	m.lines = wrap(message, m.width)
	m.offset = 0
	m.preRender()
}

// setHeight sets how many lines of the message fit, the rest scrolls (0 for all).
func (m *Model) setHeight(height int) {
	m.height = height
	m.scroll(0)
}

// overflows is a message taller than the dialog.
func (m *Model) overflows() bool {
	return m.height > 0 && len(m.lines) > m.height
}

// scroll moves the message by n lines.
func (m *Model) scroll(n int) {
	m.offset += n
	if m.overflows() {
		m.offset = max(0, min(m.offset, len(m.lines)-m.height))
	} else {
		m.offset = 0
	}
	m.preRender()
}

// SetYesKeys replaces the keys that confirm (y and enter by default), the help shows
// them all but enter.
func (m *Model) SetYesKeys(keys ...string) {
	shown := slices.DeleteFunc(slices.Clone(keys), func(k string) bool { return k == "enter" })
	m.keyMap.yes = util.BindNamedKey(strings.Join(shown, "/"), "yes", keys...)
	m.preRender()
}

// setChoices makes the dialog a choice between the bindings (esc cancels), none goes back
// to yes and no.
func (m *Model) setChoices(choices []key.Binding) {
	m.keyMap.choices = choices
	m.keyMap.no = newKeyMap().no
	if len(choices) != 0 {
		m.keyMap.no = util.Bind("cancel", "esc")
	}
	m.preRender()
}

func (m *Model) preRender() {
	title := m.styles.title.Render(m.title)
	shown := m.lines
	m.keyMap.scrolling = m.overflows()
	if m.overflows() {
		shown = m.lines[m.offset : m.offset+m.height]
	}
	parts := []string{title, " ", m.styles.message.Render(strings.Join(shown, "\n"))}
	if m.overflows() {
		var more []string
		if m.offset > 0 {
			more = append(more, "↑ more")
		}
		if m.offset+m.height < len(m.lines) {
			more = append(more, "↓ more")
		}
		parts = append(parts, m.styles.more.Render(fmt.Sprintf("%s (%d-%d of %d lines)", strings.Join(more, ", "), m.offset+1, m.offset+m.height, len(m.lines))))
	}
	helpStr := lipgloss.PlaceHorizontal(m.width, lipgloss.Center, m.help.View(m.keyMap))
	m.view = lipgloss.JoinVertical(lipgloss.Left, append(parts, " ", helpStr)...)
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

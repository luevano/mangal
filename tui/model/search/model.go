package search

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/tui/base"
)

type state int

const (
	unsearched state = iota
	searching
	searched
	searchCanceled
)

type Model struct {
	input textinput.Model
	state state
	query string

	maxWidth       int
	maxSuggestions int
	styles         styles
	keyMap         keyMap
}

// Query returns the current set query (only when the input is confirmed)
func (m *Model) Query() string {
	return m.query
}

// SetSuggestions sets the suggestions for the internal input.
func (m *Model) SetSuggestions(suggestions []string) {
	m.input.SetSuggestions(suggestions)
}

// Searching is a convenience method to check if a search is being performed.
func (m *Model) Searching() bool {
	return m.state == searching
}

// Focus sets the state to Searching and enables the input.
func (m *Model) Focus() tea.Cmd {
	m.state = searching
	m.input.CursorEnd()
	m.updateKeybinds()

	return m.input.Focus()
}

// Resize fits the input in size.Width, up to its max width.
func (m *Model) Resize(size base.Size) tea.Cmd {
	// the cursor takes a cell after the text
	m.input.Width = max(1, min(m.maxWidth, size.Width-lipgloss.Width(m.input.Prompt)-1))
	return nil
}

func (m *Model) Init() tea.Cmd {
	return textinput.Blink
}

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keyMap.confirm):
			// Remove all surrounding whitespace
			q := strings.TrimSpace(m.input.Value())
			if q == "" {
				return base.Notify("Can't search whitespace only")
			}

			m.state = searched
			m.input.Blur()
			m.updateKeybinds()

			// remove redundant spaces and use that value as the query
			q = strings.Join(strings.Fields(q), " ")
			m.input.SetValue(q)
			m.query = q
			return SearchCmd(q)
		case key.Matches(msg, m.keyMap.cancel):
			m.state = searchCanceled
			m.input.Blur()
			m.input.Reset()
			m.updateKeybinds()
			// keep the last searched query in the input field
			m.input.SetValue(m.query)

			return SearchCancelCmd
		}
	}

	input, cmd := m.input.Update(msg)
	m.input = input
	return cmd
}

func (m *Model) View() string {
	return m.input.View()
}

// SuggestionBox returns the rendered suggestions box with styling applied.
func (m *Model) SuggestionBox() string {
	// as wide as the input, with the box's margin and borders
	width := max(1, lipgloss.Width(m.input.Prompt)+m.input.Width+1-3)

	sugs, idx := m.suggestions()
	if len(sugs) == 0 {
		return m.styles.renderSuggestionBox(m.styles.normalSuggestion.Render("<no suggestions>"), width)
	}
	if len(sugs) > m.maxSuggestions {
		sugs = sugs[:m.maxSuggestions]
	}

	var sb strings.Builder
	sb.Grow(200)
	for i, s := range sugs {
		if i == idx {
			sb.WriteString(m.styles.matchSuggestion.Render(s))
		} else {
			sb.WriteString(m.styles.normalSuggestion.Render(s))
		}
		if i < len(sugs)-1 {
			sb.WriteByte('\n')
		}
	}
	return m.styles.renderSuggestionBox(sb.String(), width)
}

// suggestions are the matched suggestions and the current one's index, or all of them
// and -1 when the input is empty.
func (m *Model) suggestions() ([]string, int) {
	if strings.TrimSpace(m.input.Value()) == "" {
		return m.input.AvailableSuggestions(), -1
	}
	return m.input.MatchedSuggestions(), m.input.CurrentSuggestionIndex()
}

// updateKeybinds if the keymap should be enabled.
func (m *Model) updateKeybinds() {
	enable := m.Searching()
	m.keyMap.cancel.SetEnabled(enable)
	m.keyMap.confirm.SetEnabled(enable)
}

package search

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/tui/base"
)

func TestResizeGrowsBack(t *testing.T) {
	m := New("", "", 64, 5)
	m.Resize(base.Size{Width: 20, Height: 10})
	if w := lipgloss.Width(m.View()); w > 20 {
		t.Errorf("input %d wide in 20 cols", w)
	}
	m.Resize(base.Size{Width: 200, Height: 10})
	if m.input.Width != 64 {
		t.Errorf("input didn't grow back to its max width: %d", m.input.Width)
	}
}

func TestSuggestionBoxAsWideAsInput(t *testing.T) {
	m := New("", "", 30, 5)
	m.SetSuggestions([]string{"hunter x hunter", "one piece"})
	m.Focus()
	m.Resize(base.Size{Width: 100, Height: 10})
	// the placeholder is a bit narrower than a typed text
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("xyz")})
	if in, box := lipgloss.Width(m.View()), lipgloss.Width(m.SuggestionBox()); in != box {
		t.Errorf("input %d wide, suggestion box %d", in, box)
	}
}

func TestSuggestionsMatched(t *testing.T) {
	m := New("", "", 30, 5)
	m.SetSuggestions([]string{"hunter x hunter", "one piece", "Hunter?"})
	m.Focus()
	if sugs, idx := m.suggestions(); len(sugs) != 3 || idx != -1 {
		t.Errorf("empty input: got %v %d", sugs, idx)
	}
	for _, r := range "hun" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	sugs, idx := m.suggestions()
	if len(sugs) != 2 || idx != 0 {
		t.Errorf("got %v %d", sugs, idx)
	}
}

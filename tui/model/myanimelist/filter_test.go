package myanimelist

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/tui/base"
)

// / on the saved logins doesn't start a filter: its typing would be the screen's keys too
// (MT-A6)
func TestSavedLoginsDontFilter(t *testing.T) {
	s := New().(*state)
	s.Resize(base.Size{Width: 80, Height: 10})
	s.setStatus(statusMsg{users: []string{"nick", "bob"}})
	if s.phase != saved {
		t.Fatalf("phase %v, want the saved logins", s.phase)
	}
	s.Update(context.Background(), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if s.list.Filtering() || s.Inputting() {
		t.Error("/ started a filter")
	}
}

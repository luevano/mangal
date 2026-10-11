package confirm

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/tui/base"
)

type yesMsg struct{}

func keyPress(s string) tea.KeyMsg {
	if s == "enter" {
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestDialog(t *testing.T) {
	d := NewDialog(20, lipgloss.Color("1"))
	d.Resize(base.Size{Width: 40, Height: 10})
	onYes := func() tea.Msg { return yesMsg{} }

	d.AskDelete("Delete", "sure?", strings.Repeat(strings.Repeat(".", 40)+"\n", 9)+strings.Repeat(".", 40), onYes)
	if !d.Active() {
		t.Fatal("not active")
	}
	if v := d.View(); !strings.Contains(v, "sure?") || lipgloss.Height(v) != 10 {
		t.Errorf("view:\n%s", v)
	}
	if cmd := d.Update(keyPress("enter")); cmd != nil || !d.Active() {
		t.Error("enter confirmed a delete")
	}
	if cmd := d.Update(keyPress("d")); cmd == nil || d.Active() {
		t.Fatal("d didn't confirm")
	} else if _, ok := cmd().(yesMsg); !ok {
		t.Error("onYes not returned")
	}

	d.Ask("Download", "sure?", "", onYes)
	if cmd := d.Update(keyPress("n")); cmd != nil || d.Active() {
		t.Error("n didn't close it")
	}
	if cmd := d.Update(keyPress("y")); cmd != nil {
		t.Error("an inactive dialog answered")
	}
}

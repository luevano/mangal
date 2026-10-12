package confirm

import (
	"fmt"
	"slices"
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
	if cmd := d.Update(keyPress("D")); cmd != nil || !d.Active() {
		t.Error("D confirmed a delete")
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

type keepMsg struct{}

func TestChoose(t *testing.T) {
	d := NewDialog(30, lipgloss.Color("1"))
	d.Resize(base.Size{Width: 40, Height: 10})
	d.Choose("Title", "keep or adopt?", "", Choice{Key: "k", Label: "keep", Cmd: func() tea.Msg { return keepMsg{} }}, Choice{Key: "a", Label: "adopt"})
	if v := d.View(); !strings.Contains(v, "k keep") || !strings.Contains(v, "esc cancel") || strings.Contains(v, "yes") {
		t.Errorf("view:\n%s", v)
	}
	if cmd := d.Update(keyPress("y")); cmd != nil || !d.Active() {
		t.Error("y answered a choice")
	}
	if cmd := d.Update(keyPress("k")); cmd == nil || d.Active() {
		t.Fatal("k didn't choose")
	} else if _, ok := cmd().(keepMsg); !ok {
		t.Error("the choice's cmd not returned")
	}
	d.Choose("Title", "?", "", Choice{Key: "k", Label: "keep"})
	if d.Update(tea.KeyMsg{Type: tea.KeyEsc}); d.Active() {
		t.Error("esc didn't cancel")
	}
	// back to yes and no
	d.Ask("Download", "sure?", "", func() tea.Msg { return yesMsg{} })
	if v := d.View(); !strings.Contains(v, "yes") || !strings.Contains(v, "no") {
		t.Errorf("view after a choice:\n%s", v)
	}
	if cmd := d.Update(keyPress("y")); cmd == nil {
		t.Error("y didn't confirm")
	}
}

func TestScroll(t *testing.T) {
	d := NewDialog(30, lipgloss.Color("1"))
	d.Resize(base.Size{Width: 60, Height: 14})
	var lines []string
	for i := range 10 {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	d.Ask("Long", strings.Join(lines, "\n"), "", func() tea.Msg { return yesMsg{} })
	v := d.View()
	if !strings.Contains(v, "line 0") || strings.Contains(v, "line 9") || !strings.Contains(v, "↓ more") || !strings.Contains(v, "y yes") {
		t.Fatalf("view:\n%s", v)
	}
	d.Update(keyPress("j"))
	d.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	d.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if v := d.View(); !strings.Contains(v, "line 9") || strings.Contains(v, "line 0") || !strings.Contains(v, "↑ more") {
		t.Errorf("scrolled:\n%s", v)
	}
	// k is an answer here: it chooses, down scrolls
	d.Choose("Long", strings.Join(lines, "\n"), "", Choice{Key: "k", Label: "keep", Cmd: func() tea.Msg { return keepMsg{} }})
	if cmd := d.Update(tea.KeyMsg{Type: tea.KeyDown}); cmd != nil || !d.Active() || !strings.Contains(d.View(), "line 4") {
		t.Errorf("down didn't scroll:\n%s", d.View())
	}
	if cmd := d.Update(keyPress("k")); cmd == nil || d.Active() {
		t.Error("k scrolled instead of choosing")
	}
	// a short message doesn't scroll
	d.Ask("Short", "sure?", "", nil)
	if v := d.View(); strings.Contains(v, "more") || strings.Contains(v, "scroll") {
		t.Errorf("short:\n%s", v)
	}
}

func TestWrap(t *testing.T) {
	got := wrap("  from mango-mangadex/Auto Hunting With My Clones (2022) [alid-155950]", 40)
	want := []string{"  from mango-mangadex/Auto Hunting With", "    My Clones (2022) [alid-155950]"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q", got)
	}
	if got := wrap("  to   mango-mangasee/AutoHuntingWithMyClonesTheLongName", 30); got[0] != "  to   mango-mangasee/" {
		t.Errorf("separator: %q", got)
	}
}

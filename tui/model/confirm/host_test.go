package confirm

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/util"
)

func TestHost(t *testing.T) {
	var none Host
	if none.Asking() || !none.Backable(true) || none.Inputting(false) || none.View(func() string { return "state" }) != "state" {
		t.Error("a Host without a dialog isn't the State's")
	}
	if _, ok := none.Answer(keyPress("y")); ok {
		t.Error("a Host without a dialog answered")
	}

	h := NewHost(30, lipgloss.Color("1"))
	resized := base.Size{}
	h.Resize(base.Size{Width: 40, Height: 10}, func(size base.Size) tea.Cmd { resized = size; return nil })
	if resized.Width != 40 {
		t.Errorf("the State resized to %+v", resized)
	}
	stateKeys := util.KeyMap{Short: nil}
	if h.Backable(false) || !h.Backable(true) || !h.Inputting(true) || h.Inputting(false) || h.KeyMap(stateKeys) == nil {
		t.Error("not asking: the State's")
	}
	h.Confirm.Ask("Move", "sure?", "behind", func() tea.Msg { return yesMsg{} })
	if !h.Asking() || h.Backable(true) || !h.Inputting(false) {
		t.Error("asking: esc and the keys aren't the dialog's")
	}
	if _, ok := h.KeyMap(stateKeys).(keyMap); !ok {
		t.Errorf("asking: help %T, want the dialog's", h.KeyMap(stateKeys))
	}
	if v := h.View(func() string { t.Error("asking: drew the State"); return "" }); !strings.Contains(v, "sure?") {
		t.Errorf("view:\n%s", v)
	}
	if _, ok := h.Answer(tea.WindowSizeMsg{}); ok {
		t.Error("a msg that isn't a key answered")
	}
	cmd, ok := h.Answer(keyPress("y"))
	if !ok || cmd == nil || h.Asking() {
		t.Fatal("y didn't answer")
	}
	if _, isYes := cmd().(yesMsg); !isYes {
		t.Error("not the yes cmd")
	}
}

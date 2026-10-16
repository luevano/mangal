package huhform

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/tui/util"
)

func required(s string) error {
	if s == "" {
		return errors.New("required")
	}
	return nil
}

func press(f *Form, keys ...string) (submitted bool, cmd tea.Cmd) {
	for _, k := range keys {
		submitted, cmd = f.Update(util.KeyPress(k))
	}
	return submitted, cmd
}

func typed(f *Form, s string) {
	for _, r := range s {
		f.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// TestForm: the focused field takes the typing, tab/↓ and shift+tab/↑ move (wrapping
// around), space flips a toggle, enter validates every field and submits.
func TestForm(t *testing.T) {
	user := Text("User", "", "who you are", required)
	pass := Secret("Password", "", "", required)
	remember := Toggle("Remember", false, "")
	f := New(user, pass, remember)

	typed(f, "ino")
	press(f, "tab")
	typed(f, "pw")
	if user.Value() != "ino" || pass.Raw() != "pw" || f.Current() != 1 {
		t.Fatalf("typed %q %q, at %d", user.Value(), pass.Raw(), f.Current())
	}
	if view := f.View(); !strings.Contains(view, "ino") || strings.Contains(view, "pw") || !strings.Contains(view, "who you are") {
		t.Errorf("view:\n%s", view)
	}
	press(f, "down", "space")
	if !remember.On() || f.Current() != 2 {
		t.Errorf("space: on %v, at %d", remember.On(), f.Current())
	}
	press(f, "tab")
	if f.Current() != 0 {
		t.Errorf("tab on the last field: at %d", f.Current())
	}
	press(f, "shift+tab")
	if f.Current() != 2 {
		t.Errorf("shift+tab on the first field: at %d", f.Current())
	}
	if submitted, _ := press(f, "enter"); !submitted {
		t.Error("enter on valid fields didn't submit")
	}

	// enter on an invalid form focuses the first invalid field, typing clears its error
	pass.SetValue("")
	press(f, "up", "up")
	if submitted, _ := press(f, "enter"); submitted || f.Current() != 1 || pass.Err() != "required" {
		t.Fatalf("empty password: submitted %v, at %d, err %q", submitted, f.Current(), pass.Err())
	}
	if !strings.Contains(f.View(), "required") {
		t.Errorf("no error shown:\n%s", f.View())
	}
	typed(f, "x")
	if pass.Err() != "" {
		t.Errorf("typing kept the error %q", pass.Err())
	}
}

// TestAction: enter on an action field runs it instead of submitting.
func TestAction(t *testing.T) {
	type pressedMsg struct{}
	act := Action("Manga", "One Piece", "", util.Bind("change manga", "enter"), func() tea.Cmd {
		return func() tea.Msg { return pressedMsg{} }
	})
	f := New(act, Text("Schedule", "", "", nil))
	if keys := f.Keys(); keys[0].Help().Desc != "change manga" || keys[1].Enabled() {
		t.Errorf("action keys: %v", keys)
	}
	submitted, cmd := press(f, "enter")
	if submitted || cmd == nil {
		t.Fatalf("enter on the action: submitted %v, cmd %v", submitted, cmd)
	}
	if _, ok := cmd().(pressedMsg); !ok {
		t.Error("enter didn't run the action")
	}
	act.SetDisplay("Berserk")
	if !strings.Contains(f.View(), "Berserk") {
		t.Errorf("view:\n%s", f.View())
	}
	if submitted, _ := press(f, "tab", "enter"); !submitted {
		t.Error("enter on the text field didn't submit")
	}
}

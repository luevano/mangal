package fzf

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/luevano/mangal/v5/tui/base"
)

func send(t *testing.T, p *picker, msgs ...tea.Msg) tea.Cmd {
	t.Helper()
	var cmd tea.Cmd
	for _, msg := range msgs {
		_, cmd = p.Update(msg)
	}
	return cmd
}

func typed(s string) tea.Msg { return tea.KeyPressMsg{Code: []rune(s)[0], Text: s} }

// isQuit is true for the picker's huh.NextField, the form's submit
func isQuit(cmd tea.Cmd) bool {
	return cmd != nil && cmd() == huh.NextField()
}

// run sends msgs to the form, and the msgs of its cmds, as tea would.
func run(f *huh.Form, msgs ...tea.Msg) {
	for len(msgs) > 0 && f.State == huh.StateNormal {
		msg := msgs[0]
		msgs = msgs[1:]
		_, cmd := f.Update(msg)
		msgs = append(msgs, base.Drain(cmd)...)
	}
}

func visible(p *picker) []string {
	var titles []string
	for _, it := range p.list.VisibleItems() {
		titles = append(titles, string(it.(item)))
	}
	return titles
}

var items = []string{"one piece", "naruto", "bleach", "one punch man"}

func TestPickerPick(t *testing.T) {
	p := newPicker(items)
	send(t, p, tea.WindowSizeMsg{Width: 80, Height: 20})
	if got := visible(p); len(got) != len(items) {
		t.Fatalf("unfiltered shows %v", got)
	}

	cmd := send(t, p, tea.KeyPressMsg{Code: tea.KeyDown}, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !isQuit(cmd) || p.picked != 1 {
		t.Errorf("down+enter picked %d, quit %v", p.picked, isQuit(cmd))
	}
}

func TestPickerFilter(t *testing.T) {
	p := newPicker(items)
	send(t, p, tea.WindowSizeMsg{Width: 80, Height: 20})

	send(t, p, typed("on"), typed("e"))
	if got := visible(p); len(got) != 2 || got[0] != "one piece" {
		t.Fatalf("filter %q shows %v", p.list.FilterInput.Value(), got)
	}
	// moving in the filtered list picks the original index
	send(t, p, tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}, tea.KeyPressMsg{Code: tea.KeyEnter})
	if p.picked != 3 {
		t.Errorf("picked %d, want 3", p.picked)
	}

	// a new filter starts from the top, backspace widens it
	p = newPicker(items)
	send(t, p, typed("one"), tea.KeyPressMsg{Code: tea.KeyDown}, typed("p"))
	if got := visible(p); p.list.Index() != 0 || len(got) != 2 {
		t.Errorf("after typing: index %d, shown %v", p.list.Index(), got)
	}
	send(t, p, tea.KeyPressMsg{Code: tea.KeyBackspace}, tea.KeyPressMsg{Code: tea.KeyBackspace}, tea.KeyPressMsg{Code: tea.KeyBackspace}, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := visible(p); len(got) != len(items) {
		t.Errorf("cleared filter shows %v", got)
	}
	send(t, p, tea.KeyPressMsg{Code: tea.KeyDown}, tea.KeyPressMsg{Code: tea.KeyDown}, tea.KeyPressMsg{Code: tea.KeyEnter})
	if p.picked != 2 {
		t.Errorf("cleared filter picked %d, want 2", p.picked)
	}

	// nothing matches: enter does nothing
	p = newPicker(items)
	cmd := send(t, p, typed("zzz"), tea.KeyPressMsg{Code: tea.KeyEnter})
	if isQuit(cmd) || p.picked != -1 {
		t.Errorf("no match: quit %v, picked %d", isQuit(cmd), p.picked)
	}
}

func TestPickerCancel(t *testing.T) {
	for _, k := range []tea.KeyPressMsg{{Code: tea.KeyEscape}, {Code: 'c', Mod: tea.ModCtrl}} {
		p := newPicker(items)
		f := newForm(p)
		run(f, typed("na"), k)
		if f.State != huh.StateAborted || p.picked != -1 {
			t.Errorf("%v: state %v, picked %d", k, f.State, p.picked)
		}
	}
}

// TestPickerForm: in its huh form the picker fills the window, enter submits the form.
func TestPickerForm(t *testing.T) {
	p := newPicker(items)
	f := newForm(p)
	// the second size is tea's answer to the picker's tea.RequestWindowSize
	run(f, tea.WindowSizeMsg{Width: 60, Height: 30}, tea.WindowSizeMsg{Width: 60, Height: 30})
	if h := strings.Count(f.View(), "\n") + 1; h != 30 {
		t.Errorf("view height %d, want the window's 30", h)
	}
	run(f, typed("b"), tea.KeyPressMsg{Code: tea.KeyEnter})
	if f.State != huh.StateCompleted || p.picked != 2 {
		t.Errorf("enter: state %v, picked %d", f.State, p.picked)
	}
}

func TestPickerAccessible(t *testing.T) {
	p := newPicker(items)
	var out strings.Builder
	if err := p.RunAccessible(&out, strings.NewReader("9\n2\n")); err != nil || p.picked != 1 {
		t.Errorf("picked %d, %v", p.picked, err)
	}
	if !strings.Contains(out.String(), "4. one punch man") {
		t.Errorf("output:\n%s", out.String())
	}
}

func TestPickerWrap(t *testing.T) {
	p := newPicker(items)
	send(t, p, tea.WindowSizeMsg{Width: 80, Height: 20}, tea.KeyPressMsg{Code: tea.KeyUp}, tea.KeyPressMsg{Code: tea.KeyEnter})
	if p.picked != len(items)-1 {
		t.Errorf("up from the top picked %d", p.picked)
	}
}

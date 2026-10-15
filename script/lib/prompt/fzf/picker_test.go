package fzf

import (
	"testing"

	tea "charm.land/bubbletea/v2"
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

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
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
		cmd := send(t, p, typed("na"), k)
		if !isQuit(cmd) || p.picked != -1 {
			t.Errorf("%v: quit %v, picked %d", k, isQuit(cmd), p.picked)
		}
	}
}

func TestPickerWrap(t *testing.T) {
	p := newPicker(items)
	send(t, p, tea.WindowSizeMsg{Width: 80, Height: 20}, tea.KeyPressMsg{Code: tea.KeyUp}, tea.KeyPressMsg{Code: tea.KeyEnter})
	if p.picked != len(items)-1 {
		t.Errorf("up from the top picked %d", p.picked)
	}
}

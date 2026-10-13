package base

import (
	"reflect"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Drain runs cmd as the TUI would, for tests: the cmds of batches and sequences too, the
// results of loads unwrapped; cmds still waiting after a moment (ticks, blinks) are skipped.
// It returns the msgs that would reach the State.
func Drain(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(200 * time.Millisecond):
		return nil
	}
	switch m := msg.(type) {
	case nil, loadStartMsg:
		return nil
	case tea.BatchMsg:
		var msgs []tea.Msg
		for _, c := range m {
			msgs = append(msgs, Drain(c)...)
		}
		return msgs
	case loadDoneMsg:
		if m.msg == nil {
			return nil
		}
		return []tea.Msg{m.msg}
	case loadLabelMsg:
		return Drain(m.next)
	}
	// tea.Sequence's msg is an unexported []tea.Cmd
	if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice {
		var msgs []tea.Msg
		for i := range v.Len() {
			if c, ok := v.Index(i).Interface().(tea.Cmd); ok {
				msgs = append(msgs, Drain(c)...)
			}
		}
		return msgs
	}
	return []tea.Msg{msg}
}

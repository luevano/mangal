package base

import (
	"reflect"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Drain runs cmd as the TUI would, for tests: the cmds of batches and sequences too, the
// results of loads unwrapped (their work waited for); other cmds still waiting after a
// moment (ticks, blinks) are skipped. It returns the msgs that would reach the State.
func Drain(cmd tea.Cmd) []tea.Msg {
	msg, ok := runCmd(cmd, false)
	if !ok {
		return nil
	}
	return expand(msg)
}

// runCmd runs cmd, waiting for it if wait, else for a moment: ok is false then.
func runCmd(cmd tea.Cmd, wait bool) (tea.Msg, bool) {
	if cmd == nil {
		return nil, false
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	if wait {
		return <-done, true
	}
	select {
	case msg := <-done:
		return msg, true
	case <-time.After(200 * time.Millisecond):
		return nil, false
	}
}

// expand is what msg brings to the State.
func expand(msg tea.Msg) []tea.Msg {
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
	// tea.Sequence's msg is an unexported []tea.Cmd; a load is a sequence of its start
	// and its work
	if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeFor[tea.Cmd]() {
		var msgs []tea.Msg
		started := false
		for i := range v.Len() {
			m, ok := runCmd(v.Index(i).Interface().(tea.Cmd), started)
			_, started = m.(loadStartMsg)
			if ok {
				msgs = append(msgs, expand(m)...)
			}
		}
		return msgs
	}
	return []tea.Msg{msg}
}

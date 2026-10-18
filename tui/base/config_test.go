package base

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
)

type fakeConfigWindow struct {
	fakeState
	filters []string
}

func (w *fakeConfigWindow) Filter(filter string) tea.Cmd {
	w.filters = append(w.filters, filter)
	return nil
}

func withConfigWindow(t *testing.T) *[]*fakeConfigWindow {
	t.Helper()
	var opened []*fakeConfigWindow
	NewConfigWindow = func(filter string) State {
		w := &fakeConfigWindow{title: "config", filters: []string{filter}}
		opened = append(opened, w)
		return w
	}
	t.Cleanup(func() { NewConfigWindow = nil })
	return &opened
}

// feed runs the cmd and feeds its msg back, once.
func feed(m *model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if msg := cmd(); msg != nil {
		m.Update(msg)
	}
}

func TestConfigKey(t *testing.T) {
	opened := withConfigWindow(t)
	home := &fakeState{title: "home"}
	m := newTestModel(t, home)
	screen := &fakeState{title: "screen", inputting: true}
	m.Update(State(screen))

	// typed while inputting
	if _, cmd := m.Update(keyMsg(",")); cmd != nil || !screen.received(keyMsg(",")) {
		t.Fatal(", while inputting didn't reach the state")
	}
	screen.inputting = false
	_, cmd := m.Update(keyMsg(","))
	feed(m, cmd)
	if len(*opened) != 1 || m.state != (*opened)[0] {
		t.Fatalf("config window not shown, state %q", m.state.Title().Text)
	}
	// not another one over it
	if _, cmd := m.Update(keyMsg(",")); cmd != nil {
		feed(m, cmd)
	}
	if len(*opened) != 1 {
		t.Error("a second config window opened")
	}

	_, cmd = m.Update(BackMsg{Steps: 1})
	if m.state != screen {
		t.Errorf("back didn't return to the screen, state %q", m.state.Title().Text)
	}
	_ = cmd
}

func TestConfigErrorKey(t *testing.T) {
	opened := withConfigWindow(t)
	m := newTestModel(t, &fakeState{title: "home"})
	base := errors.New("strict")
	err := ConfigError(base, "download.metadata.strict")
	if !errors.Is(err, base) || !errors.Is(err, err) {
		t.Error("ConfigError doesn't match")
	}

	_, cmd := m.Update(err)
	feed(m, cmd)
	if !m.inViewport || m.viewportConfigKey != "download.metadata.strict" {
		t.Fatalf("error not shown with its key: %v %q", m.inViewport, m.viewportConfigKey)
	}
	_, cmd = m.Update(keyMsg("e"))
	feed(m, cmd)
	if m.inViewport {
		t.Error("viewport still shown")
	}
	if len(*opened) != 1 || (*opened)[0].filters[0] != "download.metadata.strict" {
		t.Fatalf("config window not opened on the key: %v", *opened)
	}

	// a plain error has no key
	_, cmd = m.Update(errors.New("other"))
	feed(m, cmd)
	if m.viewportConfigKey != "" || m.keyMap.configFix.Enabled() {
		t.Error("plain error with the config fix key")
	}
}

func TestReloadProviders(t *testing.T) {
	if TakeReloadProviders() {
		t.Fatal("stale at start")
	}
	ReloadProviders()
	if !TakeReloadProviders() || TakeReloadProviders() {
		t.Error("not taken once")
	}
}

package connections

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/util"
)

func TestKeys(t *testing.T) {
	s := New("", "").(*state)
	if keys := base.KeyCollisions(s.keyMap()); len(keys) != 0 {
		t.Error(keys)
	}
}

func TestRows(t *testing.T) {
	t.Cleanup(func() { _ = config.Set(config.Connections.Key, map[string]string{}) })
	if err := config.Set(config.Connections.Key, map[string]string{"nas": "http://nas.lan:6977/", "two": "http://two.lan"}); err != nil {
		t.Fatal(err)
	}
	rows := items("http://nas.lan:6977")
	if len(rows) != 3 || rows[0].name != "local" || rows[0].current || rows[1].name != "nas" || !rows[1].current || rows[2].current {
		t.Errorf("%+v", rows)
	}
	s := New("", "").(*state)
	// enter on the one in use (local) only says so, on another it switches
	if cmd := s.Update(context.Background(), util.KeyPress("enter")); cmd == nil {
		t.Fatal("no answer")
	} else if _, ok := cmd().(base.SwitchMsg); ok {
		t.Error("switched to what's in use")
	}
	s.List.Select(1)
	cmd := s.Update(context.Background(), util.KeyPress("enter"))
	if m, ok := cmd().(base.SwitchMsg); !ok || m.Value != "nas" {
		t.Errorf("%#v", cmd())
	}
}

// the picker the TUI opens at with a broken connection switches to what's "in use" too
func TestOpensWithTheError(t *testing.T) {
	s := New("http://gone.lan", "gone.lan: unreachable").(*state)
	if s.why == "" {
		t.Fatal("no error")
	}
	s.Resize(base.Size{Width: 80, Height: 20})
	if v := s.View(); !strings.Contains(v, "unreachable") {
		t.Errorf("%q", v)
	}
}

// the form's user and password are refused over plain HTTP to another machine before sending
func TestLoginForm(t *testing.T) {
	t.Cleanup(func() { _ = config.Set(config.Connections.Key, map[string]string{}) })
	if err := config.Set(config.Connections.Key, map[string]string{"nas": "http://nas.lan:6977"}); err != nil {
		t.Fatal(err)
	}
	s := New("", "").(*state)
	s.List.Select(1)
	s.Update(context.Background(), util.KeyPress("i"))
	if s.form == nil || len(s.form.Fields) != 3 {
		t.Fatal("no login form")
	}
	if host, _ := os.Hostname(); s.form.Fields[2].Value() != host {
		t.Errorf("the device is %q, not the hostname", s.form.Fields[2].Value())
	}
	s.form.Fields[0].SetValue("david")
	s.form.Fields[1].SetValue("the password")
	if cmd := s.saveToken(context.Background()); cmd != nil || !strings.Contains(s.err, "plain HTTP") {
		t.Errorf("sent: %v, %q", cmd, s.err)
	}
}

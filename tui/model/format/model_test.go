package format

import (
	"github.com/luevano/mangal/v5/tui/util"

	"testing"

	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
)

// a filter that matches no format: the keys don't set one (no item to read it from)
func TestNoMatch(t *testing.T) {
	m := New(color.Viewport)
	m.Init()
	m.list.SetFilterText("nothing like a format")
	if m.list.SelectedItem() != nil {
		t.Fatal("a format matches")
	}
	for _, k := range []string{"r", "d"} {
		if cmd := m.Update(util.KeyPress(k)); cmd != nil {
			if _, ok := cmd().(error); ok {
				t.Errorf("%s: an error", k)
			}
		}
	}
	m.Update(util.KeyPress("enter"))
}

// On a server the read format stays this machine's, the download format is the server's to
// ask for, and isn't offered to a non-admin.
func TestServer(t *testing.T) {
	base.CurrentRemote = &base.Remote{URL: "http://nas.lan:6977", Perms: core.Scopes}
	t.Cleanup(func() { base.CurrentRemote = nil })
	m := New(color.Viewport)
	m.Init()
	cmd := m.Update(util.KeyPress("d"))
	if cmd == nil {
		t.Fatal("no cmd")
	}
	if _, ok := cmd().(AskServerMsg); !ok {
		t.Errorf("%T", cmd())
	}
	base.CurrentRemote.Perms = []string{core.ScopeRead}
	m = New(color.Viewport)
	m.Init()
	if cmd := m.Update(util.KeyPress("d")); cmd != nil {
		if _, ok := cmd().(AskServerMsg); ok {
			t.Error("a reader is asked for the server's format")
		}
	}
}

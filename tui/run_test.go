package tui

import (
	"strings"
	"testing"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core/remote"
)

// A flag's or the variable's run switches for the session and says why, never saves.
func TestSwitchInSession(t *testing.T) {
	t.Cleanup(func() { _ = config.Set(config.Connections.Key, map[string]string{}) })
	if err := config.Set(config.Connections.Key, map[string]string{"nas": "http://nas.lan"}); err != nil {
		t.Fatal(err)
	}
	next, notices := switchTo(remote.Target{Remote: "http://other.lan"}, "nas")
	if next.Remote != "nas" || next.Local || len(notices) != 1 || !strings.Contains(notices[0], "this session") {
		t.Errorf("%+v %q", next, notices)
	}
	next, notices = switchTo(remote.Target{Local: true}, config.LocalConnection)
	if next.Remote != "" || !next.Local || len(notices) != 1 {
		t.Errorf("%+v %q", next, notices)
	}
	if got := config.Connection.Get(); got != config.LocalConnection {
		t.Errorf("saved %q", got)
	}
}

package base

import (
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/tui/util"
)

// every gate is a Key* action's (or a screen's enter), its feature core's, its permission one
func TestRemoteGates(t *testing.T) {
	old, newer := Keys()
	descs := []string{"providers", "open"}
	for _, b := range append(old, newer...) {
		descs = append(descs, b.Help().Desc)
	}
	for k, g := range remoteGates {
		_, desc, _ := strings.Cut(k, ":")
		if !slices.Contains(descs, desc) {
			t.Errorf("%s: no Key* binding has the help %q", k, desc)
		}
		if g.feature != "" && !slices.ContainsFunc(core.Features, func(f core.Feature) bool { return f.Name == g.feature }) {
			t.Errorf("%s: no feature %q", k, g.feature)
		}
		if g.perm != "" && !slices.Contains(core.Scopes, g.perm) {
			t.Errorf("%s: no permission %q", k, g.perm)
		}
	}
}

func TestRemoteRefusals(t *testing.T) {
	reader := &Remote{URL: "http://nas.lan:6977", Version: "5.29.0", Perms: []string{core.ScopeRead, core.ScopeProgress}, Features: core.APIFeatures()}
	admin := &Remote{URL: reader.URL, Perms: core.Scopes, Features: core.APIFeatures()}
	screen := util.KeyMap{Short: []key.Binding{KeyMerge, KeyItsWatch, KeyMove}}
	press := tea.KeyPressMsg{Code: 'J', Text: "J"}
	if why := reader.keyRefusal("library", screen, press); !strings.Contains(why, "manage permission on nas.lan:6977") {
		t.Errorf("reader's merge: %q", why)
	}
	if why := admin.keyRefusal("library", screen, press); why != "" {
		t.Errorf("admin's merge: %q", why)
	}
	if why := reader.keyRefusal("library", screen, tea.KeyPressMsg{Code: 'X', Text: "X"}); !strings.Contains(why, "manage permission") {
		t.Errorf("reader's move: %q", why)
	}
	old := &Remote{URL: reader.URL, Version: "5.20.0", Perms: core.Scopes}
	if why := old.keyRefusal("library", screen, press); !strings.Contains(why, "mangal 5.20.0) doesn't have merge") {
		t.Errorf("an older server's merge: %q", why)
	}
	shown := gatedKeyMap{keys: screen, screen: "library", r: reader}.ShortHelp()
	if len(shown) != 1 || shown[0].Help().Desc != KeyItsWatch.Help().Desc {
		t.Errorf("reader's keys: %v", shown)
	}
	if why := admin.screenRefusal("jobs"); !strings.Contains(why, "isn't on a remote server yet") {
		t.Errorf("the jobs screen on a remote server: %q", why)
	}
	if why := admin.screenRefusal("chapters/p/m"); why != "" {
		t.Errorf("the chapters screen: %q", why)
	}
	if why := admin.screenRefusal("library/12"); why != "" {
		t.Errorf("a library manga: %q", why)
	}
}

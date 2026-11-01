package base

import (
	"encoding/json"
	"github.com/luevano/mangal/v5/core/remote"
	"net/http"
	"net/http/httptest"
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
	// what a remote can't do says so as a fact, no "yet"
	open := tea.KeyPressMsg{Code: 'o', Text: "o"}
	if why := admin.keyRefusal("download/1", util.KeyMap{Short: []key.Binding{KeyOpenDir}}, open); why != "A server's folder isn't on this machine" {
		t.Errorf("open directory: %q", why)
	}
	// the accounts: any of the permissions that have rows there
	accounts := util.KeyMap{Short: []key.Binding{KeyAccounts}}
	k := tea.KeyPressMsg{Code: 'K', Text: "K"}
	if why := reader.keyRefusal("home", accounts, k); why != "" {
		t.Errorf("a reader's accounts: %q", why)
	}
	browser := &Remote{URL: reader.URL, Perms: []string{core.ScopeRead}, Features: core.APIFeatures()}
	if why := browser.keyRefusal("home", accounts, k); !strings.Contains(why, "one of the progress, tokens, admin permissions") {
		t.Errorf("a read token's accounts: %q", why)
	}
}

// a source an admin enables shows with the server's settings, no switch
func TestMetadataSourcesRefresh(t *testing.T) {
	enabled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{{"key": "kitsu", "code": "kt", "name": "Kitsu", "enabled": enabled}})
	}))
	defer srv.Close()
	rc, err := remote.New(remote.Options{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	r := &Remote{URL: srv.URL, Client: rc}
	if r.metadataEnabled("kt") {
		t.Error("enabled before it was read")
	}
	r.RefreshMetadataSources(t.Context())
	if r.metadataEnabled("kt") {
		t.Error("enabled while off")
	}
	enabled = true
	r.RefreshMetadataSources(t.Context())
	if !r.metadataEnabled("kt") {
		t.Error("not enabled after the refresh")
	}
}

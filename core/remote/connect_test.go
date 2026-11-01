package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/luevano/mangal/v5/config"
)

func TestResolve(t *testing.T) {
	config.Reset()
	t.Cleanup(config.Reset)
	if err := config.Set(config.Connections.Key, map[string]string{"nas": "http://nas.lan:6977/"}); err != nil {
		t.Fatal(err)
	}
	for value, want := range map[string]string{
		"":                "",
		"local":           "",
		"nas":             "http://nas.lan:6977",
		"NAS":             "http://nas.lan:6977",
		"https://x.lan/":  "https://x.lan",
		"http://x.lan:80": "http://x.lan",
	} {
		if got, err := Resolve(value); err != nil || got != want {
			t.Errorf("%q: %q, %v, want %q", value, got, err, want)
		}
	}
	for _, value := range []string{"typo", "nas.lan:6977", "ftp://x"} {
		if got, err := Resolve(value); err == nil {
			t.Errorf("%q: %q, want an error", value, got)
		}
	}
	if got := Name("http://nas.lan:6977"); got != "nas" {
		t.Errorf("name %q", got)
	}
	if got := Name("https://other.lan:1"); got != "other.lan:1" {
		t.Errorf("name %q", got)
	}
}

func TestTarget(t *testing.T) {
	config.Reset()
	t.Cleanup(config.Reset)
	config.Set(config.Connections.Key, map[string]string{"nas": "http://nas.lan:6977"})
	config.Set(config.Connection.Key, "nas")
	for _, tc := range []struct {
		target Target
		want   string
	}{
		{Target{}, "http://nas.lan:6977"},
		{Target{Local: true}, ""},
		{Target{Remote: "http://other.lan"}, "http://other.lan"},
	} {
		if got, err := tc.target.Address(); err != nil || got != tc.want {
			t.Errorf("%+v: %q, %v", tc.target, got, err)
		}
	}
}

// remote.token only goes to the connection key's server, never to a --remote elsewhere.
func TestTokenOnlyToTheConnection(t *testing.T) {
	config.Reset()
	t.Cleanup(config.Reset)
	config.Set(config.Connections.Key, map[string]string{"nas": "http://nas.lan:6977"})
	config.Set(config.Connection.Key, "nas")
	config.Set(config.Remote.Token.Key, "secret")
	if token, from, err := Token("http://nas.lan:6977"); err != nil || token != "secret" || from != "remote.token" {
		t.Errorf("%q %q %v", token, from, err)
	}
	// nas isn't the connection any more: its saved token (none here) or nothing
	config.Set(config.Connection.Key, config.LocalConnection)
	if configured, err := Resolve(config.Connection.Get()); err != nil || configured != "" {
		t.Errorf("%q %v", configured, err)
	}
}

// On a server config answers the server's keys, keeps this machine's own and writes neither
// the server's nor a value the server hides.
func TestConfigStore(t *testing.T) {
	config.Reset()
	t.Cleanup(config.Reset)
	skipHome := config.TUI.SkipHome.Get()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"key": "download.format", "value": "PDF", "default": "CBZ", "description": "", "stored": "db", "source": map[string]any{"kind": "db"}, "applies": "now", "secret": false},
			{"key": "tui.skip_home", "value": !skipHome, "default": skipHome, "description": "", "stored": "file", "source": map[string]any{"kind": "file"}, "applies": "now", "secret": false},
		})
	}))
	defer srv.Close()
	c, err := New(Options{URL: srv.URL, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ReadConfig(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := config.Download.Format.Get().String(); got != "PDF" {
		t.Errorf("download.format %s", got)
	}
	if config.TUI.SkipHome.Get() != skipHome {
		t.Error("tui.skip_home is this machine's")
	}
	err = config.WriteKey(config.Download.Format.Key, "CBZ")
	if err == nil || !strings.Contains(err.Error(), "setting") {
		t.Errorf("write of a server's key: %v", err)
	}
	if err := c.Config.Set("download.format", nil, false, ""); err == nil {
		t.Error("the store writes")
	}
}

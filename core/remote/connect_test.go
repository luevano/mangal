package remote

import (
	"testing"

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

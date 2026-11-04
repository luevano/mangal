package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/adrg/xdg"
	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/watch"
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

// The metadata search goes to the server, with its ranking, and asks for an id as id:<n>.
func TestMetadataThroughTheServer(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Path+"?"+r.URL.Query().Get("q"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{{"source": "al", "id": "5", "title": "Berserk", "url": "u", "score": 0.9, "bad": false}})
	}))
	defer srv.Close()
	c, err := New(Options{URL: srv.URL, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	src, _ := client.ParseMetadataSource("anilist")
	hits, err := client.SearchMetadataSource(context.Background(), src, c.Metadata(src), "berserk")
	if err != nil || len(hits) != 1 || hits[0].Meta.ID().Raw != "5" || hits[0].Score != 0.9 || !lmmeta.IsPartial(hits[0].Meta) {
		t.Fatalf("%+v, %v", hits, err)
	}
	if _, found, err := c.Metadata(src).SearchByIDContext(context.Background(), 5); !found || err != nil {
		t.Fatal(found, err)
	}
	want := []string{"/api/v1/metadata/anilist/search?berserk", "/api/v1/metadata/anilist/search?id:5"}
	if !slices.Equal(queries, want) {
		t.Errorf("%q, want %q", queries, want)
	}
}

// this data's serve is found by its discovery file, one of another API major can't be used, and
// none is no error
func TestLocalServe(t *testing.T) {
	old := xdg.StateHome
	xdg.StateHome = t.TempDir()
	t.Cleanup(func() { xdg.StateHome = old })
	if c, err := LocalServe(t.Context()); c != nil || err != nil {
		t.Fatalf("no serve: %v, %v", c, err)
	}
	version := "1.1.0"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"api_version": version, "ok": true, "status": "ok"})
	}))
	defer srv.Close()
	data := watch.DataDB()
	if err := watch.WriteDiscovery(watch.DiscoveryPath(data), watch.Discovery{PID: os.Getpid(), Address: srv.URL, Data: data, Key: "the key"}); err != nil {
		t.Fatal(err)
	}
	c, err := LocalServe(t.Context())
	if err != nil || c == nil {
		t.Fatalf("serve: %v, %v", c, err)
	}
	if !c.HasToken() {
		t.Error("without the discovery key")
	}
	version = "2.0.0"
	if c, err := LocalServe(t.Context()); c != nil || err == nil || !strings.Contains(err.Error(), "API 2.0.0") {
		t.Errorf("another major: %v, %v", c, err)
	}
}

// a password goes over HTTPS or to this machine, nothing is sent otherwise
func TestLoginAddress(t *testing.T) {
	for url, ok := range map[string]bool{
		"https://nas.lan":       true,
		"http://localhost:6977": true,
		"http://127.0.0.1:6977": true,
		"http://[::1]:6977":     true,
		"http://nas.lan:6977":   false,
		"http://192.168.1.2":    false,
		"http://localhost.evil": false,
		"http://127.0.0.1.evil": false,
	} {
		if err := CheckPasswordAddress(url); (err == nil) != ok {
			t.Errorf("%s: %v", url, err)
		}
	}
	asked := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { asked = true }))
	defer srv.Close()
	// the fake server's address as another machine's, through a proxy nothing goes to
	t.Setenv("HTTP_PROXY", srv.URL)
	if _, err := Login(t.Context(), "http://nas.lan:6977", "david", "the password", "", nil); err == nil || !strings.Contains(err.Error(), "plain HTTP") {
		t.Errorf("over plain HTTP: %v", err)
	}
	c, err := New(Options{URL: "http://nas.lan:6977"})
	if err != nil {
		t.Fatal(err)
	}
	users := c.Services().Users
	if _, err := users.Add(t.Context(), "kid", "the password", nil); err == nil || !strings.Contains(err.Error(), "plain HTTP") {
		t.Errorf("a new user over plain HTTP: %v", err)
	}
	if err := users.SetPassword(t.Context(), "kid", "the password"); err == nil || !strings.Contains(err.Error(), "plain HTTP") {
		t.Errorf("a new password over plain HTTP: %v", err)
	}
	if asked {
		t.Error("the password was sent")
	}
}

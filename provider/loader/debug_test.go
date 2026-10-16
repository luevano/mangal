package loader

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/luevano/mangal/v5/log"
)

func TestHTTPClientDebug(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	get := func() {
		resp, err := HTTPClient().Get(server.URL + "/api/title?secret=paid&access_token=tok&id=1")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	logged := func() []string {
		var lines []string
		for _, e := range log.Recent() {
			if strings.Contains(e.Message, server.URL) {
				lines = append(lines, e.Message)
			}
		}
		return lines
	}
	t.Cleanup(func() { log.Init(log.Options{Level: log.LevelInfo}) })

	log.Init(log.Options{Level: log.LevelInfo})
	if HTTPClient().Transport != nil {
		t.Error("without --debug the client should keep the default transport")
	}
	get()
	if lines := logged(); len(lines) != 0 {
		t.Errorf("without --debug: %q", lines)
	}

	log.Init(log.Options{Level: log.LevelDebug})
	get()
	lines := logged()
	if len(lines) != 1 {
		t.Fatalf("with --debug: %q", lines)
	}
	if want := "http: GET " + server.URL + "/api/title?access_token=REDACTED&id=1&secret=REDACTED 200 "; !strings.HasPrefix(lines[0], want) {
		t.Errorf("got %q, want %q + duration", lines[0], want)
	}

	t.Setenv("MANGOPLUS_UNSAFE_SHOW_SECRET", "1")
	get()
	if lines := logged(); len(lines) != 2 || !strings.Contains(lines[1], "secret=paid") || strings.Contains(lines[1], "=tok") {
		t.Errorf("with the unsafe env: %q", lines)
	}
}

// the providers' cache hits and headless pages go to the same log as the requests
func TestProviderDebug(t *testing.T) {
	t.Setenv("MANGAL_PROVIDERS_MANGAPLUS_SECRET_FILE", "")
	t.Cleanup(func() { log.Init(log.Options{Level: log.LevelInfo}) })
	log.Init(log.Options{Level: log.LevelInfo})
	if mangoOptions().Debug != nil || LuaOptions(t.TempDir()).Debug != nil {
		t.Error("without --debug the providers should get no debug hook")
	}

	log.Init(log.Options{Level: log.LevelDebug})
	debug := mangoOptions().Debug
	if debug == nil || LuaOptions(t.TempDir()).Debug == nil {
		t.Fatal("with --debug the providers should get a debug hook")
	}
	debug("mango-test: volumes %s cached", "key-volumes")
	var found bool
	for _, e := range log.Recent() {
		found = found || e.Message == "http: mango-test: volumes key-volumes cached"
	}
	if !found {
		t.Error("cache hit not logged")
	}
}

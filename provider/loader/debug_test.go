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

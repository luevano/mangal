package serve

import (
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/luevano/mangal/v5/web"
)

// TestRequestLog: a line per API request with who and from where, never the query or the
// token; refusals at warn with why; the web UI's files only at debug.
func TestRequestLog(t *testing.T) {
	s := newTestServer(t, "s3cret")
	s.api.Auth.Tokens = func() ([]cache.ServeTokenEntry, error) {
		return []cache.ServeTokenEntry{{Name: "claude", Hash: HashToken("r3ad"), Scopes: []string{ScopeRead}}}, nil
	}
	addUser(t, s, "david", "correct horse", Scopes...)
	browser, err := web.NewBrowser(io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	s.api.Browser = browser
	app, err := s.api.NewApp(AppOptions{Web: true, Guard: s.api.Auth.guard(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 6977})})
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	log.SetOutput(&out)
	t.Cleanup(func() { log.SetOutput(nil) })

	const remote = "192.0.2.1:1234"
	logged := func(method, path, body, authorization string, want int) string {
		t.Helper()
		out.Reset()
		if rec := do(app, method, path, body, remote, authorization, nil); rec.Code != want {
			t.Fatalf("%s %s: %d %s, want %d", method, path, rec.Code, rec.Body, want)
		}
		return out.String()
	}

	line := logged("GET", "/api/v1/watches?q=secret", "", "Bearer r3ad", http.StatusOK)
	if !strings.HasPrefix(line, "GET /api/v1/watches 200 ") || !strings.HasSuffix(line, " by token claude from 192.0.2.1\n") {
		t.Errorf("api line %q", line)
	}
	line = logged("GET", "/api/v1/watches", "", "Bearer nope", http.StatusUnauthorized)
	if !strings.HasPrefix(line, "warning: GET /api/v1/watches 401 ") || !strings.HasSuffix(line, " from 192.0.2.1: wrong API token\n") || strings.Count(line, "\n") != 1 {
		t.Errorf("refused token line %q", line)
	}
	line = logged("POST", "/api/v1/auth/login", `{"name":"david","password":"nope"}`, "", http.StatusUnauthorized)
	if !strings.Contains(line, `401`) || !strings.Contains(line, `wrong name or password ("david")`) || strings.Contains(line, "nope") {
		t.Errorf("refused login line %q", line)
	}
	line = logged("POST", "/api/v1/auth/login", `{"name":"david","password":"correct horse"}`, "", http.StatusOK)
	if !strings.Contains(line, "david logged in from 192.0.2.1") || !strings.Contains(line, "POST /api/v1/auth/login 200 ") {
		t.Errorf("login lines %q", line)
	}
	if line = logged("GET", "/", "", "", http.StatusOK); line != "" {
		t.Errorf("static file logged at info: %q", line)
	}
	if line = logged("GET", "/api/v1/status", "", "Bearer r3ad", http.StatusOK); line != "" {
		t.Errorf("polled read logged at info: %q", line)
	}

	log.Init(log.Options{Level: log.LevelDebug})
	t.Cleanup(func() { log.Init(log.Options{Level: log.LevelInfo}) })
	if line = logged("GET", "/", "", "", http.StatusOK); !strings.HasPrefix(line, "debug: GET / 200 ") {
		t.Errorf("static file at debug: %q", line)
	}
}

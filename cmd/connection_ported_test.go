package cmd

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/spf13/cobra"
)

const fakeStatus = `{"api_version":"1.1.0","ok":true,"status":"ok"}`

// fakeAPI is a connection to a server answering "METHOD /path" with the JSON; the requests it
// got, with their bodies, are in asked.
func fakeAPI(t *testing.T, answers map[string]string) *[]string {
	t.Helper()
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		key := r.Method + " " + strings.TrimPrefix(r.URL.Path, "/api/v1")
		asked = append(asked, strings.TrimSpace(key+" "+string(body)))
		w.Header().Set("Content-Type", "application/json")
		answer, ok := answers[key]
		switch {
		case key == "GET /status":
			answer = fakeStatus
		case key == "GET /config":
			answer = "[]"
		case !ok:
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"not for you","code":"forbidden"}`))
			return
		}
		if answer == "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// the jobs' endpoints
		if strings.HasSuffix(key, "/library/urls") || strings.HasSuffix(key, "/library/scan") {
			w.WriteHeader(http.StatusAccepted)
		}
		_, _ = w.Write([]byte(answer))
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { remoteArgs.Remote = "" })
	remoteArgs.Remote = srv.URL
	t.Cleanup(func() {
		s, err := cache.Settings()
		if err == nil {
			err = config.UseLocal(s)
		}
		if err != nil {
			t.Error(err)
		}
	})
	return &asked
}

// run runs the command, its stdout is the answer.
func run(t *testing.T, c *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&errOut)
	c.SetContext(t.Context())
	t.Cleanup(func() { c.SetOut(nil); c.SetErr(nil) })
	err := c.RunE(c, args)
	return out.String(), err
}

// what's ported asks the server, its answer is what's printed, nothing is this machine's
func TestPortedCommands(t *testing.T) {
	asked := fakeAPI(t, map[string]string{
		"GET /users":       `[{"name":"kid","perms":["read"],"role":"reader","password":true,"admin":false,"created":"2026-11-01T00:00:00Z","local":true}]`,
		"GET /tokens":      `[{"name":"tui@box","scopes":["read"],"role":"reader","owner":"kid","created":"2026-11-01T00:00:00Z"}]`,
		"DELETE /tokens/x": "",
		"GET /providers":   `[{"id":"mango-mangadex","name":"MangaDex","version":"1","website":"https://mangadex.org","mirrors":["https://a.example"],"preferred":"https://a.example"}]`,
		"GET /bindings":    `[{"provider":"p","manga_id":"m","source":"al","id":"30013","default":true}]`,
		"GET /permissions": `{"permissions":[{"name":"read","line":"the server's line"}],"roles":[{"name":"reader","line":"the server's role","perms":["read"]}]}`,
	})
	for _, tc := range []struct {
		name string
		cmd  *cobra.Command
		args []string
		want string
	}{
		{"user ls", userLsCmd, nil, "kid  reader: read"},
		{"token ls", tokenLsCmd, nil, "tui@box"},
		{"user roles", userRolesCmd, nil, "the server's role"},
	} {
		out, err := run(t, tc.cmd, tc.args...)
		if err != nil || !strings.Contains(out, tc.want) {
			t.Errorf("%s: %q, %v", tc.name, out, err)
		}
	}
	if out, _ := run(t, userLsCmd); !strings.Contains(out, "the server's machine's user") {
		t.Errorf("user ls says this machine: %q", out)
	}
	if err := providersLsCmd.Flags().Set("mirrors", "true"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = providersLsCmd.Flags().Set("mirrors", "false") })
	if out, err := run(t, providersLsCmd); err != nil || !strings.Contains(out, "mango-mangadex") || !strings.Contains(out, "a.example (preferred)") {
		t.Errorf("providers ls: %q, %v", out, err)
	}
	if out, err := run(t, bindLsCmd); err != nil || !strings.Contains(out, "AniList 30013") {
		t.Errorf("bind ls: %q, %v", out, err)
	}
	if _, err := run(t, tokenRmCmd, "x"); err != nil {
		t.Errorf("token rm: %v", err)
	}
	// a refusal is the server's, nothing happens here
	if _, err := run(t, userRmCmd, "kid"); err == nil || !strings.Contains(err.Error(), "not for you") {
		t.Errorf("user rm: %v", err)
	}
	if got := strings.Join(*asked, "\n"); !strings.Contains(got, "DELETE /tokens/x") || !strings.Contains(got, "DELETE /users/kid") {
		t.Errorf("asked:\n%s", got)
	}
}

// a retitle's search is the server's
func TestRetitleOnAServer(t *testing.T) {
	retitled := `{"plan":{"kind":"retitle","summary":"retitle","hash":"h","lines":["a line"],"nothing":false,"detail":{"Provider":{"id":"p"},"From":{"title":"T"}}}}`
	asked := fakeAPI(t, map[string]string{
		"POST /library/mangas/12/retitle": retitled,
	})
	if err := libraryRetitleCmd.Flags().Set("from", "p"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { libraryRetitleArgs.From = "" })
	_, err := run(t, libraryRetitleCmd, "12", "Dark", "Gathering")
	got := strings.Join(*asked, "\n")
	if !strings.Contains(got, `"provider":"p"`) || !strings.Contains(got, `"query":"Dark Gathering"`) || !strings.Contains(got, `"selector":"exact"`) {
		t.Errorf("asked:\n%s\n%v", got, err)
	}
}

func TestLibraryURLsOnAServer(t *testing.T) {
	fakeAPI(t, map[string]string{
		"POST /library/urls": `{"id":3,"kind":"source-urls","title":"source urls","status":"done","providers":[],"added":"2026-11-01T00:00:00Z","message":"2 found, 0 gone, 0 failed"}`,
	})
	if out, err := run(t, libraryURLsCmd); err != nil || !strings.Contains(out, "2 found") {
		t.Errorf("%q, %v", out, err)
	}
}

func TestScanCheckOnAServer(t *testing.T) {
	asked := fakeAPI(t, map[string]string{
		"POST /library/scan": `{"id":3,"kind":"scan","title":"library scan","status":"done","providers":[],"added":"2026-11-01T00:00:00Z","message":"0 chapters added, 2 folders' ids to check"}`,
	})
	libraryScanArgs.Check = true
	t.Cleanup(func() { libraryScanArgs.Check = false })
	out, err := run(t, libraryScanCmd)
	if err != nil || !strings.Contains(out, "ids to check") || !strings.Contains(out, "on the server's machine") {
		t.Errorf("%q, %v", out, err)
	}
	if got := strings.Join(*asked, "\n"); !strings.Contains(got, `"check":true`) {
		t.Errorf("asked:\n%s", got)
	}
}

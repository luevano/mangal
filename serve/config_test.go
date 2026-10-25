package serve

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// TestV1ConfigAdmin: every user with read sees the settings but the risky ones (paths,
// network, security, the log, secrets), which only an admin sees; only an admin (or the
// server's own mangal) changes or undoes any; a startup key's change is "restart needed"
// until the server restarts; a change is undone from the history, once.
func TestV1ConfigAdmin(t *testing.T) {
	s, _, _ := newHealthServer(t)
	path := filepath.Join(t.TempDir(), "mangal.toml")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	saved := config.Path
	if err := config.Load(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { config.Path = saved })
	addUser(t, s, "reader", "a long password", ScopeRead)
	addUser(t, s, "writer", "a long password", ScopeRead, ScopeWrite)
	addUser(t, s, "admin", "a long password", Scopes...)
	h := s.handler(t)
	as := func(user string) func(method, path, body string, out any) int {
		auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":a long password"))
		return func(method, path, body string, out any) int {
			t.Helper()
			rec := do(h, method, path, body, "127.0.0.1:1", auth, nil)
			if out != nil && rec.Code == http.StatusOK {
				if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
					t.Fatal(err)
				}
			}
			return rec.Code
		}
	}
	asReader, asWriter, asAdmin := as("reader"), as("writer"), as("admin")
	keys := func(entries []apiv1.ConfigEntry) []string {
		var out []string
		for _, e := range entries {
			out = append(out, e.Key)
		}
		return out
	}

	hidden := []string{"serve.auth", "download.path", "download.mode_file_decimal", "log.level", "data.path", "providers.headless.flaresolverr_url"}
	for name, call := range map[string]func(string, string, string, any) int{"admin": asAdmin, "local": func(m, p, b string, o any) int { return s.call(t, m, p, b, o) }} {
		var all []apiv1.ConfigEntry
		if code := call("GET", "/api/v1/config", "", &all); code != http.StatusOK {
			t.Fatalf("%s's config %d", name, code)
		}
		for _, key := range hidden {
			if !slices.Contains(keys(all), key) {
				t.Errorf("%s doesn't see %s", name, key)
			}
		}
	}
	for name, call := range map[string]func(string, string, string, any) int{"reader": asReader, "writer": asWriter} {
		var some []apiv1.ConfigEntry
		if code := call("GET", "/api/v1/config", "", &some); code != http.StatusOK || len(some) == 0 {
			t.Fatalf("%s's config %d", name, code)
		}
		for _, e := range some {
			if config.IsRisky(e.Key) || slices.Contains(hidden, e.Key) || e.Secret {
				t.Errorf("the %s sees %s", name, e.Key)
			}
		}
		for _, key := range []string{"download.format", "watch.schedule", "jobs.check.enabled", "notification.tui"} {
			if !slices.Contains(keys(some), key) {
				t.Errorf("the %s doesn't see %s", name, key)
			}
		}
		// no change of any setting, risky or not, a setting-like one neither
		for _, put := range []struct{ method, path, body string }{
			{"PUT", "/api/v1/config/download.skip_if_exists", `{"value":false}`},
			{"PUT", "/api/v1/config/download.path", `{"value":"/tmp/x"}`},
			{"PUT", "/api/v1/config/serve.port", `{"value":7000}`},
			{"PUT", "/api/v1/config/jobs.check.enabled", `{"value":false}`},
			{"POST", "/api/v1/config/history/1/undo", ""},
			{"PUT", "/api/v1/providers/fake/mirror", `{"mirror":""}`},
			{"PUT", "/api/v1/jobs/kinds/check/settings", `{"enabled":false}`},
		} {
			if code := call(put.method, put.path, put.body, nil); code != http.StatusForbidden {
				t.Errorf("%s %s by the %s: %d", put.method, put.path, name, code)
			}
		}
	}

	// a startup key: restart needed, for the admin only (it's risky too)
	var e apiv1.ConfigEntry
	if code := s.call(t, "PUT", "/api/v1/config/serve.session_lifetime", `{"value":"1h"}`, &e); code != http.StatusOK || e.Applies != "restart" || e.Restart == nil || !*e.Restart {
		t.Fatalf("serve.session_lifetime by the admin: %d %+v", code, e)
	}
	var st apiv1.Status
	if code := s.call(t, "GET", "/api/v1/status", "", &st); code != http.StatusOK || st.RestartNeeded == nil || !slices.Contains(*st.RestartNeeded, "serve.session_lifetime") {
		t.Errorf("admin's status %d %+v", code, st.RestartNeeded)
	}
	st = apiv1.Status{}
	if code := asWriter("GET", "/api/v1/status", "", &st); code != http.StatusOK || st.RestartNeeded != nil {
		t.Errorf("writer's status %d %+v", code, st.RestartNeeded)
	}
	e = apiv1.ConfigEntry{}
	if code := s.call(t, "PUT", "/api/v1/config/serve.session_lifetime", `{"value":"720h"}`, &e); code != http.StatusOK || e.Restart != nil {
		t.Errorf("back to the start value: %d %+v", code, e)
	}

	// history and undo
	if code := asAdmin("PUT", "/api/v1/config/download.skip_if_exists", `{"value":false}`, nil); code != http.StatusOK {
		t.Fatalf("download.skip_if_exists by the admin: %d", code)
	}
	if code := s.call(t, "PUT", "/api/v1/config/providers.headless.flaresolverr_url", `{"value":"http://x.example:8191/v1"}`, nil); code != http.StatusOK {
		t.Fatalf("flaresolverr by the local mangal: %d", code)
	}
	var history []apiv1.ConfigChange
	if code := asReader("GET", "/api/v1/config/history", "", &history); code != http.StatusOK || len(history) == 0 {
		t.Fatalf("reader's history %d %+v", code, history)
	}
	ids := map[string]int64{}
	for _, c := range history {
		if config.IsRisky(c.Key) {
			t.Errorf("the reader sees the change of %s", c.Key)
		}
		if _, ok := ids[c.Key]; !ok {
			ids[c.Key] = c.Id
		}
	}
	history = nil
	if code := asAdmin("GET", "/api/v1/config/history?key=providers.headless.flaresolverr_url", "", &history); code != http.StatusOK || len(history) == 0 {
		t.Fatalf("admin's history %d", code)
	}
	risky := history[0].Id
	if code := asWriter("POST", fmt.Sprintf("/api/v1/config/history/%d/undo", risky), "", nil); code != http.StatusForbidden {
		t.Errorf("undo of a risky change by a writer: %d", code)
	}
	if code := asAdmin("POST", fmt.Sprintf("/api/v1/config/history/%d/undo", risky), "", &e); code != http.StatusOK || e.Value != e.Default {
		t.Errorf("undo by the admin: %d %+v", code, e)
	}
	undo := fmt.Sprintf("/api/v1/config/history/%d/undo", ids["download.skip_if_exists"])
	if code := asWriter("POST", undo, "", nil); code != http.StatusForbidden {
		t.Errorf("undo by the writer: %d", code)
	}
	if code := s.call(t, "POST", undo, "", &e); code != http.StatusOK || e.Value != true {
		t.Errorf("undo by the local mangal: %d %+v", code, e)
	}
	if code := asAdmin("POST", undo, "", nil); code != http.StatusBadRequest {
		t.Errorf("the same undo again: %d", code)
	}
}

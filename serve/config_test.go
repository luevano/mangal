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

// TestV1ConfigAdmin: the risky keys are an admin's only, even to see; a startup key's
// change is "restart needed" until the server restarts; a change is undone from the
// history, once.
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
	addUser(t, s, "writer", "a long password", ScopeRead, ScopeWrite)
	h := s.handler(t)
	writer := "Basic " + base64.StdEncoding.EncodeToString([]byte("writer:a long password"))
	asWriter := func(method, path, body string, out any) int {
		t.Helper()
		rec := do(h, method, path, body, "127.0.0.1:1", writer, nil)
		if out != nil && rec.Code == http.StatusOK {
			if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
				t.Fatal(err)
			}
		}
		return rec.Code
	}
	keys := func(entries []apiv1.ConfigEntry) []string {
		var out []string
		for _, e := range entries {
			out = append(out, e.Key)
		}
		return out
	}

	var all, some []apiv1.ConfigEntry
	if code := s.call(t, "GET", "/api/v1/config", "", &all); code != http.StatusOK || !slices.Contains(keys(all), "serve.auth") {
		t.Fatalf("admin's config %d, serve.auth missing", code)
	}
	if code := asWriter("GET", "/api/v1/config", "", &some); code != http.StatusOK || len(some) == 0 {
		t.Fatalf("writer's config %d", code)
	}
	for _, e := range some {
		if config.IsRisky(e.Key) {
			t.Errorf("the writer sees %s", e.Key)
		}
	}
	if !slices.Contains(keys(some), "download.format") {
		t.Error("the writer doesn't see download.format")
	}
	if code := asWriter("PUT", "/api/v1/config/serve.port", `{"value":7000}`, nil); code != http.StatusNotFound {
		t.Errorf("serve.port by a writer: %d", code)
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
	if code := asWriter("PUT", "/api/v1/config/download.skip_if_exists", `{"value":false}`, nil); code != http.StatusOK {
		t.Fatalf("download.skip_if_exists by a writer: %d", code)
	}
	if code := s.call(t, "PUT", "/api/v1/config/providers.headless.flaresolverr_url", `{"value":"http://x.example:8191/v1"}`, nil); code != http.StatusOK {
		t.Fatalf("flaresolverr by the admin: %d", code)
	}
	var history []apiv1.ConfigChange
	if code := asWriter("GET", "/api/v1/config/history", "", &history); code != http.StatusOK || len(history) == 0 {
		t.Fatalf("writer's history %d %+v", code, history)
	}
	ids := map[string]int64{}
	for _, c := range history {
		if config.IsRisky(c.Key) {
			t.Errorf("the writer sees the change of %s", c.Key)
		}
		if _, ok := ids[c.Key]; !ok {
			ids[c.Key] = c.Id
		}
	}
	history = nil
	if code := s.call(t, "GET", "/api/v1/config/history?key=providers.headless.flaresolverr_url", "", &history); code != http.StatusOK || len(history) == 0 {
		t.Fatalf("admin's history %d", code)
	}
	risky := history[0].Id
	if code := asWriter("POST", fmt.Sprintf("/api/v1/config/history/%d/undo", risky), "", nil); code != http.StatusNotFound {
		t.Errorf("undo of a risky change by a writer: %d", code)
	}
	if code := s.call(t, "POST", fmt.Sprintf("/api/v1/config/history/%d/undo", risky), "", &e); code != http.StatusOK || e.Value != e.Default {
		t.Errorf("undo by the admin: %d %+v", code, e)
	}
	undo := fmt.Sprintf("/api/v1/config/history/%d/undo", ids["download.skip_if_exists"])
	if code := asWriter("POST", undo, "", &e); code != http.StatusOK || e.Value != true {
		t.Errorf("undo by the writer: %d %+v", code, e)
	}
	if code := asWriter("POST", undo, "", nil); code != http.StatusBadRequest {
		t.Errorf("the same undo again: %d", code)
	}
}

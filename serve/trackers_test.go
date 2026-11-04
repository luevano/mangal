package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// TestAPIClients: an admin reads, sets and removes the server's AniList and MyAnimeList API
// clients, s3cr3t is never answered; anyone else is refused; the AniList client follows
// metadata.anilist.redirect, which an admin sets too.
func TestAPIClients(t *testing.T) {
	s := newTestServer(t, "")
	path := filepath.Join(t.TempDir(), "mangal.toml")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	saved := config.Path
	if err := config.Load(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { config.Path = saved })
	addUser(t, s, "admin", "a long password", Scopes...)
	addUser(t, s, "manager", "a long password", ScopeRead, ScopeProgress, ScopeBrowse, ScopeDownload, ScopeManage, ScopeTokens)
	h := s.handler(t)
	admin, manager := basic("admin", "a long password"), basic("manager", "a long password")
	call := func(auth, method, path, body string) *http.Response {
		t.Helper()
		rec := do(h, method, path, body, "192.0.2.1:1", auth, nil)
		if strings.Contains(rec.Body.String(), "s3cr3t") {
			t.Errorf("%s %s answers s3cr3t: %s", method, path, rec.Body)
		}
		return rec.Result()
	}
	list := func() map[string]apiv1.APIClient {
		t.Helper()
		rec := do(h, "GET", "/api/v1/accounts/clients", "", "192.0.2.1:1", admin, nil)
		var l []apiv1.APIClient
		if err := json.Unmarshal(rec.Body.Bytes(), &l); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("list %d %s", rec.Code, rec.Body)
		}
		out := map[string]apiv1.APIClient{}
		for _, c := range l {
			out[string(c.Source)] = c
		}
		return out
	}

	if l := list(); len(l) != 2 || l["anilist"].Configured || l["myanimelist"].Configured || deref(l["anilist"].Redirect) != "local" {
		t.Fatalf("fresh %+v", l)
	}
	for _, r := range []struct{ method, path, body string }{
		{"GET", "/api/v1/accounts/clients", ""},
		{"PUT", "/api/v1/accounts/clients/anilist", `{"id":"1"}`},
		{"DELETE", "/api/v1/accounts/clients/myanimelist", ""},
	} {
		if res := call(manager, r.method, r.path, r.body); res.StatusCode != http.StatusForbidden {
			t.Errorf("manager %s %s: %d", r.method, r.path, res.StatusCode)
		}
	}

	if res := call(admin, "PUT", "/api/v1/accounts/clients/myanimelist", `{"id":" abc ","secret":"s3cr3t"}`); res.StatusCode != http.StatusOK {
		t.Fatalf("set %d", res.StatusCode)
	}
	if id, secret, _ := cache.MyAnimeListClient(); id != "abc" || secret != "s3cr3t" {
		t.Errorf("saved %q %q", id, secret)
	}
	// the same id without a secret keeps it, another id drops it
	call(admin, "PUT", "/api/v1/accounts/clients/myanimelist", `{"id":"abc"}`)
	if l := list()["myanimelist"]; !l.Configured || deref(l.Id) != "abc" || !l.Secret {
		t.Errorf("kept %+v", l)
	}
	call(admin, "PUT", "/api/v1/accounts/clients/myanimelist", `{"id":"def"}`)
	if l := list()["myanimelist"]; deref(l.Id) != "def" || l.Secret {
		t.Errorf("another id %+v", l)
	}
	if res := call(admin, "PUT", "/api/v1/accounts/clients/anilist", `{"id":""}`); res.StatusCode != http.StatusBadRequest {
		t.Errorf("empty id %d", res.StatusCode)
	}

	call(admin, "PUT", "/api/v1/accounts/clients/anilist", `{"id":"123","secret":"s3cr3t"}`)
	if res := call(admin, "PUT", "/api/v1/config/metadata.anilist.redirect", `{"value":"pin"}`); res.StatusCode != http.StatusOK {
		t.Fatalf("redirect %d", res.StatusCode)
	}
	if l := list()["anilist"]; deref(l.Id) != "123" || !l.Secret || deref(l.Redirect) != "pin" || l.RedirectUrl != "https://anilist.co/api/v2/oauth/pin" {
		t.Errorf("anilist %+v", l)
	}
	rec := do(h, "GET", "/api/v1/me/accounts", "", "192.0.2.1:1", admin, nil)
	if !strings.Contains(rec.Body.String(), `"ready":true`) {
		t.Errorf("not ready: %s", rec.Body)
	}

	if res := call(admin, "DELETE", "/api/v1/accounts/clients/anilist", ""); res.StatusCode != http.StatusNoContent {
		t.Errorf("remove %d", res.StatusCode)
	}
	if id, secret, _ := cache.AnilistClient(); id != "" || secret != "" {
		t.Errorf("still saved %q %q", id, secret)
	}
	if res := call(admin, "PUT", "/api/v1/accounts/clients/kitsu", `{"id":"1"}`); res.StatusCode != http.StatusBadRequest {
		t.Errorf("kitsu %d", res.StatusCode)
	}
}

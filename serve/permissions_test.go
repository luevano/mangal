package serve

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/luevano/mangal/v5/web/apiv1"
)

func basic(name, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(name+":"+password))
}

// TestOwnTokens: the tokens permission makes your own tokens, never with more than you
// have, and you only see and remove yours.
func TestOwnTokens(t *testing.T) {
	s := newTestServer(t, "")
	s.api.Users = core.NewUsersOf(storage.Users{DB: s.svc.Watches.DB})
	s.api.Auth.Tokens = cache.ServeTokens
	addUser(t, s, "kid", "a long password", ScopeRead, ScopeBrowse, ScopeTokens)
	addUser(t, s, "guest", "a long password", ScopeRead)
	h := s.handler(t)
	kid, guest := basic("kid", "a long password"), basic("guest", "a long password")
	const remote = "192.0.2.1:1"

	if rec := do(h, "POST", "/api/v1/tokens", `{"name":"x"}`, remote, guest, nil); rec.Code != http.StatusForbidden {
		t.Errorf("without tokens: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/v1/tokens", `{"name":"x","role":"downloader"}`, remote, kid, nil); rec.Code != http.StatusForbidden {
		t.Errorf("more than kid has: %d %s", rec.Code, rec.Body)
	}
	rec := do(h, "POST", "/api/v1/tokens", `{"name":"kids","scopes":["browse"]}`, remote, kid, nil)
	var made apiv1.NewTokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &made); rec.Code != http.StatusCreated || err != nil || deref(made.Token.Owner) != "kid" {
		t.Fatalf("kid's token: %d %s", rec.Code, rec.Body)
	}
	if _, _, err := s.api.Users.AddToken(t.Context(), "servers", "", []string{ScopeRead}); err != nil {
		t.Fatal(err)
	}
	var list []apiv1.Token
	rec = do(h, "GET", "/api/v1/tokens", "", remote, kid, nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list) != 1 || list[0].Name != "kids" {
		t.Errorf("kid sees %s", rec.Body)
	}
	if rec := do(h, "DELETE", "/api/v1/tokens/servers", "", remote, kid, nil); rec.Code != http.StatusNotFound {
		t.Errorf("someone else's: %d %s", rec.Code, rec.Body)
	}
	// the token works as kid, with browse at most
	if rec := do(h, "GET", "/api/v1/watches", "", remote, "Bearer "+made.Secret, nil); rec.Code != http.StatusOK {
		t.Errorf("kid's token: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "DELETE", "/api/v1/tokens/kids", "", remote, kid, nil); rec.Code != http.StatusNoContent {
		t.Errorf("remove: %d %s", rec.Code, rec.Body)
	}
}

// TestUserPositions: each user keeps their own reading position, admins the server's.
func TestUserPositions(t *testing.T) {
	s := newTestServer(t, "")
	addUser(t, s, "a", "a long password", ScopeRead, ScopeProgress)
	addUser(t, s, "b", "a long password", ScopeRead, ScopeProgress)
	h := s.handler(t)
	put := func(who string, page int) {
		body := fmt.Sprintf(`{"chapter":"c","page":%d}`, page)
		if rec := do(h, "PUT", "/api/v1/reader/position", body, "192.0.2.1:1", who, nil); rec.Code != http.StatusOK {
			t.Fatalf("put: %d %s", rec.Code, rec.Body)
		}
	}
	get := func(who string) int {
		var pos apiv1.ReaderPosition
		rec := do(h, "GET", "/api/v1/reader/position?chapter=c", "", "192.0.2.1:1", who, nil)
		if err := json.Unmarshal(rec.Body.Bytes(), &pos); err != nil {
			t.Fatalf("get: %d %s", rec.Code, rec.Body)
		}
		return pos.Page
	}
	a, b := basic("a", "a long password"), basic("b", "a long password")
	put(a, 3)
	put(b, 5)
	if get(a) != 3 || get(b) != 5 || get("Bearer "+testLocalKey) != 0 {
		t.Errorf("positions a %d, b %d, server %d", get(a), get(b), get("Bearer "+testLocalKey))
	}
}

// TestHidePaths: the server's directories become library-relative paths or placeholders,
// and a relative chapter path is taken back.
func TestHidePaths(t *testing.T) {
	lib := path.Downloads()
	r := pathReplacer()
	in := `{"path":"` + filepath.Join(lib, "Manga", "1.cbz") + `","dir":"` + lib + `","db":"` + filepath.Join(path.Data(), "mangal.sqlite") + `"}`
	want := `{"path":"Manga/1.cbz","dir":"<library>","db":"<data>/mangal.sqlite"}`
	if got := r.Replace(in); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	if got := libraryPath("Manga/1.cbz"); got != filepath.Join(lib, "Manga", "1.cbz") {
		t.Errorf("library path %s", got)
	}
}

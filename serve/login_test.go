package serve

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// addUser adds a user to the test server's data db.
func addUser(t *testing.T, s *testServer, name, password string, perms ...string) {
	t.Helper()
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if err := (storage.Users{DB: s.svc.Watches.DB}).Add(storage.User{Name: name, Hash: hash, Perms: perms}); err != nil {
		t.Fatal(err)
	}
}

// do sends a request to h as from remote, with the cookie and the authorization when set.
func do(h http.Handler, method, path, body, remote, authorization string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host, req.RemoteAddr = "127.0.0.1:6977", remote
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func sessionOf(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	t.Fatalf("no session cookie: %d %s", rec.Code, rec.Body)
	return nil
}

// TestLogin: a user logs in (the cookie), is who /auth/me says, has their permissions; a
// new password ends the login, logout too; the same machine is no different.
func TestLogin(t *testing.T) {
	s := newTestServer(t, "s3cret")
	addUser(t, s, "David", "correct horse", Scopes...)
	addUser(t, s, "guest", "battery staple", ScopeRead)
	h := s.handler(t)
	const local, remote = "127.0.0.1:1234", "192.0.2.1:1234"

	for _, from := range []string{local, remote} {
		if rec := do(h, "GET", "/api/v1/watches", "", from, "", nil); rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") == "Basic" {
			t.Errorf("nobody from %s: %d %v", from, rec.Code, rec.Header())
		}
	}
	if rec := do(h, "GET", "/api/v1/auth/me", "", remote, "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("me, nobody: %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/v1/auth/login", `{"name":"david","password":"wrong"}`, remote, "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong password: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/v1/auth/login", `{"name":"nobody","password":"correct horse"}`, remote, "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("unknown user: %d %s", rec.Code, rec.Body)
	}
	// names are case-insensitive
	rec := do(h, "POST", "/api/v1/auth/login", `{"name":"david","password":"correct horse","remember":true}`, remote, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	cookie := sessionOf(t, rec)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Secure || cookie.MaxAge <= 0 {
		t.Errorf("cookie %+v", cookie)
	}
	var me apiv1.Me
	rec = do(h, "GET", "/api/v1/auth/me", "", remote, "", cookie)
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil || me.Name != "David" || me.Kind != "user" || len(me.Scopes) != len(Scopes) {
		t.Errorf("me: %d %+v %v", rec.Code, me, err)
	}
	if rec := do(h, "GET", "/api/v1/watches", "", remote, "", cookie); rec.Code != http.StatusOK {
		t.Errorf("with the session: %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/v1/history/clear?apply=1", `{"expect":"x"}`, remote, "", cookie); rec.Code != http.StatusConflict {
		t.Errorf("write with the session: %d %s", rec.Code, rec.Body)
	}

	// the guest reads, nothing else; without remember the cookie ends with the browser
	rec = do(h, "POST", "/api/v1/auth/login", `{"name":"guest","password":"battery staple"}`, remote, "", nil)
	guest := sessionOf(t, rec)
	if guest.MaxAge != 0 || !guest.Expires.IsZero() {
		t.Errorf("not remembered: %+v", guest)
	}
	if rec := do(h, "GET", "/api/v1/watches", "", remote, "", guest); rec.Code != http.StatusOK {
		t.Errorf("guest reads: %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/v1/check", `{"dry_run":true}`, remote, "", guest); rec.Code != http.StatusForbidden {
		t.Errorf("guest checks: %d %s", rec.Code, rec.Body)
	}

	// a new password (another process: the users are read again) ends the sessions
	hash, _ := HashPassword("new password!")
	if err := (storage.Users{DB: s.svc.Watches.DB}).SetHash("david", hash); err != nil {
		t.Fatal(err)
	}
	s.api.Auth.users = nil
	if rec := do(h, "GET", "/api/v1/watches", "", remote, "", cookie); rec.Code != http.StatusUnauthorized {
		t.Errorf("after the new password: %d", rec.Code)
	}

	// logout ends the guest's session and deletes the cookie
	rec = do(h, "POST", "/api/v1/auth/logout", "", remote, "", guest)
	if rec.Code != http.StatusNoContent || sessionOf(t, rec).MaxAge >= 0 {
		t.Errorf("logout: %d %v", rec.Code, rec.Result().Cookies())
	}
	if rec := do(h, "GET", "/api/v1/watches", "", remote, "", guest); rec.Code != http.StatusUnauthorized {
		t.Errorf("after logout: %d", rec.Code)
	}

	// the web UI's files are public (here the UI is off: 404, not 401)
	if rec := do(h, "GET", "/", "", remote, "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("the UI: %d", rec.Code)
	}
}

// TestSecureCookie: HTTPS (here a proxy's X-Forwarded-Proto) makes the cookie Secure.
func TestSecureCookie(t *testing.T) {
	s := newTestServer(t, "s3cret")
	addUser(t, s, "david", "correct horse", Scopes...)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"name":"david","password":"correct horse"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	s.handler(t).ServeHTTP(rec, req)
	if c := sessionOf(t, rec); !c.Secure {
		t.Errorf("cookie %+v", c)
	}
}

// TestLoginRateLimit: failed logins are slowed down, the right password waits too.
func TestLoginRateLimit(t *testing.T) {
	s := newTestServer(t, "s3cret")
	addUser(t, s, "david", "correct horse", Scopes...)
	h := s.handler(t)
	for range freeFailures {
		if rec := do(h, "POST", "/api/v1/auth/login", `{"name":"david","password":"nope"}`, "192.0.2.1:1", "", nil); rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure: %d", rec.Code)
		}
	}
	rec := do(h, "POST", "/api/v1/auth/login", `{"name":"david","password":"correct horse"}`, "192.0.2.1:1", "", nil)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("limited: %d %v", rec.Code, rec.Header())
	}
	// basic auth too
	if rec := do(h, "GET", "/api/v1/watches", "", "192.0.2.1:1", "Basic ZGF2aWQ6Y29ycmVjdCBob3JzZQ==", nil); rec.Code != http.StatusTooManyRequests {
		t.Errorf("basic auth limited: %d", rec.Code)
	}
	// another client isn't
	if rec := do(h, "POST", "/api/v1/auth/login", `{"name":"david","password":"correct horse"}`, "198.51.100.1:1", "", nil); rec.Code != http.StatusOK {
		t.Errorf("another client: %d %s", rec.Code, rec.Body)
	}

	var l limiter
	now := time.Now()
	for range loginBurst {
		l.failed("ip", "k", now)
	}
	if w := l.wait("ip", "other", now); w <= 0 || w > loginEvery {
		t.Errorf("burst used: wait %s", w)
	}
	if w := l.wait("ip", "other", now.Add(loginEvery)); w != 0 {
		t.Errorf("a token back: wait %s", w)
	}
}

// TestAPITokens: an API token's scopes, a wrong token, the local key; a token isn't a
// password; serve.auth none lets everyone in.
func TestAPITokens(t *testing.T) {
	s := newTestServer(t, "s3cret")
	s.api.Auth.Tokens = func() ([]cache.ServeTokenEntry, error) {
		return []cache.ServeTokenEntry{
			{Name: "claude", Hash: HashToken("r3ad"), Scopes: []string{ScopeRead}},
			{Name: "phone", Hash: HashToken("pr0g"), Scopes: []string{ScopeRead, ScopeProgress}},
		}, nil
	}
	h := s.handler(t)
	const remote = "192.0.2.1:1234"
	for _, tc := range []struct {
		method, path, body, authorization string
		want                              int
	}{
		{"GET", "/api/v1/watches", "", "Bearer nope", http.StatusUnauthorized},
		{"GET", "/api/v1/watches", "", "Bearer r3ad", http.StatusOK},
		{"GET", "/api/v1/watches", "", "Bearer " + testLocalKey, http.StatusOK},
		// a token isn't a password
		{"GET", "/api/v1/watches", "", "Basic bWU6cjNhZA==", http.StatusUnauthorized},
		// a plan is a read
		{"POST", "/api/v1/history/clear", `{}`, "Bearer r3ad", http.StatusOK},
		{"POST", "/api/v1/history/clear?apply=1", `{"expect":"x"}`, "Bearer r3ad", http.StatusForbidden},
		{"POST", "/api/v1/history/clear?apply=1", `{"expect":"x"}`, "Bearer " + testLocalKey, http.StatusConflict},
		{"POST", "/api/check", `{"dry_run":true}`, "Bearer r3ad", http.StatusForbidden},
		{"POST", "/api/v1/reads", `{"provider":"fake","chapter":2}`, "Bearer pr0g", http.StatusBadRequest},
		{"POST", "/api/v1/check", `{"dry_run":true}`, "Bearer pr0g", http.StatusForbidden},
	} {
		if rec := do(h, tc.method, tc.path, tc.body, remote, tc.authorization, nil); rec.Code != tc.want {
			t.Errorf("%s %s %q: %d %s, want %d", tc.method, tc.path, tc.authorization, rec.Code, rec.Body, tc.want)
		}
	}
	var me apiv1.Me
	rec := do(h, "GET", "/api/v1/auth/me", "", remote, "Bearer pr0g", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil || me.Name != "phone" || me.Kind != "token" {
		t.Errorf("me: %+v %v", me, err)
	}

	s.api.Auth.Mode = AuthNone
	if rec := do(h, "POST", "/api/v1/history/clear?apply=1", `{"expect":"x"}`, remote, "", nil); rec.Code != http.StatusConflict {
		t.Errorf("auth none: %d %s", rec.Code, rec.Body)
	}
}

func TestPublicPaths(t *testing.T) {
	for path, want := range map[string]bool{
		"/": true, "/library/3": true, "/assets/index.js": true, "/api/v1/auth/login": true, "/api/v1/auth/me": true,
		"/api/v1/watches": false, "/api/status": false, "/api/v1/image": false, "/opds": false, "/opds/library": false, "/opdsx": true,
	} {
		if got := isPublic(path); got != want {
			t.Errorf("%s: %t", path, got)
		}
	}
}

func TestAuthMode(t *testing.T) {
	for in, want := range map[string]string{"": AuthLogin, "login": AuthLogin, "token": AuthLogin, "None": AuthNone} {
		if got, err := authMode(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	if _, err := authMode("magic"); err == nil {
		t.Error("magic: no error")
	}
}

// TestUsersAPI: only an admin manages the users and tokens; the last admin stays; a token
// made there works at once.
func TestUsersAPI(t *testing.T) {
	s := newTestServer(t, "s3cret")
	s.api.Users = core.NewUsersOf(storage.Users{DB: s.svc.Watches.DB})
	s.api.Auth.Tokens = cache.ServeTokens
	addUser(t, s, "david", "correct horse", Scopes...)
	addUser(t, s, "guest", "battery staple", ScopeRead, ScopeWrite)
	h := s.handler(t)
	const remote = "192.0.2.1:1234"
	admin := "Basic " + base64.StdEncoding.EncodeToString([]byte("david:correct horse"))
	guest := "Basic " + base64.StdEncoding.EncodeToString([]byte("guest:battery staple"))

	if rec := do(h, "GET", "/api/v1/users", "", remote, guest, nil); rec.Code != http.StatusForbidden {
		t.Errorf("guest lists: %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/v1/users", `{"name":"x","password":"long enough"}`, remote, guest, nil); rec.Code != http.StatusForbidden {
		t.Errorf("guest adds: %d", rec.Code)
	}
	var users []apiv1.User
	rec := do(h, "GET", "/api/v1/users", "", remote, admin, nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &users); err != nil || len(users) != 2 || !users[0].Admin || users[1].Admin {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/v1/users", `{"name":"kid","password":"long enough"}`, remote, admin, nil); rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"perms":["read","progress"]`) {
		t.Errorf("add: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/v1/users", `{"name":"kid","password":"long enough"}`, remote, admin, nil); rec.Code != http.StatusConflict {
		t.Errorf("add twice: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "PATCH", "/api/v1/users/david", `{"perms":["read"]}`, remote, admin, nil); rec.Code != http.StatusConflict {
		t.Errorf("demote the last admin: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "PATCH", "/api/v1/users/kid", `{"password":"another one!"}`, remote, admin, nil); rec.Code != http.StatusNoContent {
		t.Errorf("password: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "PATCH", "/api/v1/users/nobody", `{"perms":["read"]}`, remote, admin, nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown user: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/v1/users/kid/logout", "", remote, admin, nil); rec.Code != http.StatusNoContent {
		t.Errorf("logout: %d", rec.Code)
	}
	if rec := do(h, "DELETE", "/api/v1/users/kid", "", remote, admin, nil); rec.Code != http.StatusNoContent {
		t.Errorf("remove: %d", rec.Code)
	}

	var made apiv1.NewTokenResponse
	rec = do(h, "POST", "/api/v1/tokens", `{"name":"script","scopes":["read"]}`, remote, admin, nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &made); err != nil || rec.Code != http.StatusCreated || made.Secret == "" {
		t.Fatalf("token: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/v1/watches", "", remote, "Bearer "+made.Secret, nil); rec.Code != http.StatusOK {
		t.Errorf("the new token: %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/v1/tokens", "", remote, "Bearer "+made.Secret, nil); rec.Code != http.StatusForbidden {
		t.Errorf("a read token lists tokens: %d", rec.Code)
	}
	if rec := do(h, "DELETE", "/api/v1/tokens/script", "", remote, admin, nil); rec.Code != http.StatusNoContent {
		t.Errorf("remove the token: %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/v1/watches", "", remote, "Bearer "+made.Secret, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("the removed token: %d", rec.Code)
	}
}

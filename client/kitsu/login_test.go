package kitsu

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/adrg/xdg"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/libmangal/metadata/kitsu"
	"github.com/luevano/mangal/v5/util/cache"
)

func TestMain(m *testing.M) {
	// keep the logins away from the user's databases
	dir, err := os.MkdirTemp("", "mangal-kitsu-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("MANGAL_CACHE_PATH", filepath.Join(dir, "cache"))
	os.Setenv("MANGAL_AUTH_KEYRING", "false")
	xdg.DataHome = filepath.Join(dir, "data")
	httpClient = func() *http.Client { return &http.Client{Transport: fake} }
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// fakeKitsu answers the token and user requests: the valid access tokens have a user,
// the valid refresh tokens give the next access token.
type fakeKitsu struct {
	mu       sync.Mutex
	access   map[string]bool
	refresh  map[string]string
	password string
	// requests are the token grants, in order
	requests []string
}

var fake = &fakeKitsu{}

func (f *fakeKitsu) reset(password string, access []string, refresh map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.password, f.refresh, f.requests = password, refresh, nil
	f.access = map[string]bool{}
	for _, a := range access {
		f.access[a] = true
	}
}

func (f *fakeKitsu) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	respond := func(status int, body any) (*http.Response, error) {
		data, _ := json.Marshal(body)
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(data))), Header: http.Header{}, Request: req}, nil
	}
	invalid := map[string]string{"error": "invalid_grant", "error_description": "nope"}
	newToken := func(access, refresh string) (*http.Response, error) {
		f.access[access] = true
		return respond(200, kitsu.Token{AccessToken: access, RefreshToken: refresh, ExpiresIn: 30 * 24 * 3600, CreatedAt: time.Now().Unix()})
	}

	switch {
	case strings.HasSuffix(req.URL.Path, "/oauth/token"):
		_ = req.ParseForm()
		grant := req.PostForm.Get("grant_type")
		f.requests = append(f.requests, grant)
		if grant == "password" {
			if req.PostForm.Get("password") != f.password {
				return respond(400, invalid)
			}
			return newToken("a1", "r1")
		}
		next, ok := f.refresh[req.PostForm.Get("refresh_token")]
		if !ok {
			return respond(400, invalid)
		}
		return newToken(next, "r-"+next)
	case strings.HasSuffix(req.URL.Path, "/users"):
		data := []any{}
		if f.access[strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")] {
			data = append(data, map[string]any{"id": "7", "type": "users", "attributes": map[string]any{"name": "me", "slug": "me"}})
		}
		return respond(200, map[string]any{"data": data})
	}
	return respond(404, map[string]any{})
}

// start is a fresh run: logged out of the provider, nothing saved.
func start(t *testing.T) {
	t.Helper()
	k, err := client()
	if err != nil {
		t.Fatal(err)
	}
	if k.Authenticated() {
		_ = k.Logout()
	}
	ResetLogin()
	users, _ := Users()
	for _, u := range users {
		if err := Forget(u); err != nil {
			t.Fatal(err)
		}
	}
	if err := cache.SetKitsuLoggedOut(false); err != nil {
		t.Fatal(err)
	}
}

func save(t *testing.T, token kitsu.Token) {
	t.Helper()
	if err := cache.SetKitsuAuthData("me", token); err != nil {
		t.Fatal(err)
	}
	if err := account.UpdateUsers(func(h *cache.UserHistory) { h.Add("me") }); err != nil {
		t.Fatal(err)
	}
}

func saved(t *testing.T) kitsu.Token {
	t.Helper()
	var token kitsu.Token
	if found, err := cache.GetKitsuAuthData("me", &token); err != nil || !found {
		t.Fatalf("saved token: %v, %v", found, err)
	}
	return token
}

func TestLoginPasswordSavesTheToken(t *testing.T) {
	start(t)
	fake.reset("secret", nil, nil)
	ctx := context.Background()

	if _, err := LoginPassword(ctx, "me@example.com", "wrong"); !errors.Is(err, metadata.ErrUnauthorized) {
		t.Fatalf("wrong password: %v", err)
	}
	if users, _ := Users(); len(users) != 0 {
		t.Fatalf("saved a failed login: %v", users)
	}

	u, err := LoginPassword(ctx, " me@example.com ", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if u.Name() != "me" {
		t.Errorf("user %q", u.Name())
	}
	token := saved(t)
	if token.AccessToken != "a1" || token.RefreshToken != "r1" {
		t.Errorf("saved %+v", token)
	}
	// only the token is saved
	login, found, err := SavedLogin()
	if err != nil || !found || login.User != "me" || login.Expired() {
		t.Fatalf("saved login: %+v, %v, %v", login, found, err)
	}
}

func TestEnsureLoginRefreshesSoonToExpire(t *testing.T) {
	start(t)
	fake.reset("", []string{"old"}, map[string]string{"r0": "new"})
	save(t, kitsu.Token{AccessToken: "old", RefreshToken: "r0", ExpiresIn: 3600, CreatedAt: time.Now().Unix()})

	if err := EnsureLogin(context.Background()); err != nil {
		t.Fatal(err)
	}
	k, _ := client()
	if !k.Authenticated() {
		t.Fatal("not logged in")
	}
	if token := saved(t); token.AccessToken != "new" || token.RefreshToken != "r-new" {
		t.Errorf("saved %+v", token)
	}
}

func TestEnsureLoginRefreshesRejected(t *testing.T) {
	start(t)
	// the access token is no longer valid, though not expired
	fake.reset("", nil, map[string]string{"r0": "new"})
	save(t, kitsu.Token{AccessToken: "old", RefreshToken: "r0", ExpiresIn: 30 * 24 * 3600, CreatedAt: time.Now().Unix()})

	if err := EnsureLogin(context.Background()); err != nil {
		t.Fatal(err)
	}
	if token := saved(t); token.AccessToken != "new" {
		t.Errorf("saved %+v", token)
	}
}

func TestEnsureLoginExpiresRejectedRefresh(t *testing.T) {
	start(t)
	fake.reset("", nil, nil)
	save(t, kitsu.Token{AccessToken: "old", RefreshToken: "r0", ExpiresIn: 3600, CreatedAt: time.Now().Add(-2 * time.Hour).Unix()})

	if err := EnsureLogin(context.Background()); !errors.Is(err, metadata.ErrUnauthorized) {
		t.Fatalf("got %v", err)
	}
	login, found, err := SavedLogin()
	if err != nil || !found || !login.Expired() {
		t.Fatalf("saved login after the rejected refresh: %+v, %v, %v", login, found, err)
	}
	// not tried again
	ResetLogin()
	fake.reset("", nil, nil)
	if err := EnsureLogin(context.Background()); err != nil || len(fake.requests) != 0 {
		t.Errorf("tried the expired login: %v, %v", err, fake.requests)
	}
	var expired *ExpiredError
	if _, err := LoginSaved(context.Background(), "me"); !errors.As(err, &expired) {
		t.Errorf("login saved: %v", err)
	}
}

func TestLogoutSticks(t *testing.T) {
	start(t)
	fake.reset("", []string{"x"}, nil)
	save(t, kitsu.Token{AccessToken: "x", RefreshToken: "r", ExpiresIn: 30 * 24 * 3600, CreatedAt: time.Now().Unix()})
	if err := EnsureLogin(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := Logout(); err != nil {
		t.Fatal(err)
	}
	k, _ := client()
	if k.Authenticated() {
		t.Error("still logged in")
	}
	// what the next run would log in with
	if _, found, err := SavedLogin(); err != nil || found {
		t.Fatalf("saved login after the logout: %v, %v", found, err)
	}
	if users, _ := Users(); len(users) != 1 {
		t.Errorf("the user is still saved: %v", users)
	}

	// a login undoes it
	if _, err := LoginSaved(context.Background(), "me"); err != nil {
		t.Fatal(err)
	}
	if _, found, err := SavedLogin(); err != nil || !found {
		t.Fatalf("saved login after logging in again: %v, %v", found, err)
	}
}

// TestResetLoginDropsTheSession: a logout elsewhere (a TUI while serve runs) logs this
// process out on the next EnsureLogin.
func TestResetLoginDropsTheSession(t *testing.T) {
	start(t)
	fake.reset("", []string{"x"}, nil)
	save(t, kitsu.Token{AccessToken: "x", RefreshToken: "r", ExpiresIn: 30 * 24 * 3600, CreatedAt: time.Now().Unix()})
	if err := EnsureLogin(context.Background()); err != nil {
		t.Fatal(err)
	}
	k, _ := client()
	if !k.Authenticated() {
		t.Fatal("not logged in")
	}

	if err := cache.SetKitsuLoggedOut(true); err != nil {
		t.Fatal(err)
	}
	ResetLogin()
	if err := EnsureLogin(context.Background()); err != nil {
		t.Fatal(err)
	}
	if k.Authenticated() {
		t.Error("still logged in after the logout elsewhere")
	}
}

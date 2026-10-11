package myanimelist

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/adrg/xdg"
	"github.com/luevano/libmangal/metadata/myanimelist"
	"github.com/luevano/mangal/v5/auth/oauth"
	"github.com/luevano/mangal/v5/util/cache"
	"golang.org/x/oauth2"
)

func TestMain(m *testing.M) {
	// keep the logins away from the user's databases
	dir, err := os.MkdirTemp("", "mangal-mal-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("MANGAL_CACHE_PATH", filepath.Join(dir, "cache"))
	os.Setenv("MANGAL_AUTH_KEYRING", "false")
	xdg.DataHome = filepath.Join(dir, "data")
	httpClient = func() *http.Client { return &http.Client{Transport: fake} }
	if err := cache.SetMyAnimeListClient("test-id", ""); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = cache.Close()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// fakeMAL answers the token and user requests: the valid access tokens have a user, the
// valid refresh tokens give the next access token, the code "the-code" gives "a1".
type fakeMAL struct {
	mu      sync.Mutex
	access  map[string]bool
	refresh map[string]string
	// requests are the token grants, in order
	requests []string
	form     url.Values
	query    string
	// badClient answers the searches like MyAnimeList does to an unknown client id
	badClient bool
	searches  int
}

var fake = &fakeMAL{}

func (f *fakeMAL) reset(access []string, refresh map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refresh, f.requests, f.form, f.query, f.badClient, f.searches = refresh, nil, nil, "", false, 0
	f.access = map[string]bool{}
	for _, a := range access {
		f.access[a] = true
	}
}

func (f *fakeMAL) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	respond := func(status int, body any) (*http.Response, error) {
		data, _ := json.Marshal(body)
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(data))), Header: http.Header{"Content-Type": {"application/json"}}, Request: req}, nil
	}
	newToken := func(access, refresh string) (*http.Response, error) {
		f.access[access] = true
		return respond(200, map[string]any{"access_token": access, "refresh_token": refresh, "token_type": "Bearer", "expires_in": 2678400})
	}
	invalid := map[string]string{"error": "invalid_grant"}

	switch {
	case req.URL.Host == "myanimelist.net" && req.URL.Path == "/v1/oauth2/token":
		_ = req.ParseForm()
		f.form = req.PostForm
		grant := req.PostForm.Get("grant_type")
		f.requests = append(f.requests, grant)
		if grant == "authorization_code" {
			if req.PostForm.Get("code") != "the-code" || req.PostForm.Get("code_verifier") == "" {
				return respond(400, invalid)
			}
			return newToken("a1", "r1")
		}
		next, ok := f.refresh[req.PostForm.Get("refresh_token")]
		if !ok {
			return respond(400, invalid)
		}
		return newToken(next, "r-"+next)
	case req.URL.Path == "/v2/users/@me":
		if !f.access[strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")] {
			return respond(401, map[string]string{"error": "invalid_token"})
		}
		return respond(200, map[string]any{"id": 7, "name": "me"})
	case req.URL.Path == "/v2/manga":
		f.query = req.URL.Query().Get("q")
		f.searches++
		if f.badClient {
			return respond(400, map[string]string{"error": "invalid_request", "message": "Invalid client id"})
		}
		return respond(200, map[string]any{"data": []any{}})
	}
	return respond(404, map[string]any{})
}

// start is a fresh run: logged out of the provider, nothing saved.
func start(t *testing.T) {
	t.Helper()
	m, err := client()
	if err != nil {
		t.Fatal(err)
	}
	if m.Authenticated() {
		_ = m.Logout()
	}
	loginTried = false
	users, _ := Users()
	for _, u := range users {
		if err := Forget(u); err != nil {
			t.Fatal(err)
		}
	}
	if err := cache.SetMyAnimeListLoggedOut(false); err != nil {
		t.Fatal(err)
	}
}

func save(t *testing.T, token oauth2.Token) {
	t.Helper()
	if err := cache.SetMyAnimeListAuthData("me", &token); err != nil {
		t.Fatal(err)
	}
	if err := updateUsers(func(h *cache.UserHistory) { h.Add("me") }); err != nil {
		t.Fatal(err)
	}
}

func saved(t *testing.T) oauth2.Token {
	t.Helper()
	var token oauth2.Token
	if found, err := cache.GetMyAnimeListAuthData("me", &token); err != nil || !found {
		t.Fatalf("saved token: %v, %v", found, err)
	}
	return token
}

func TestLoginManualSavesTheToken(t *testing.T) {
	start(t)
	fake.reset(nil, nil)
	pasted := make(chan string, 1)
	u, err := LoginManual(context.Background(), oauth.Manual{
		Pasted: pasted,
		Show: func(authURL string) {
			u, _ := url.Parse(authURL)
			pasted <- oauth.MyAnimeListServerCallbackURL + "?code=the-code&state=" + u.Query().Get("state")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if u.Name() != "me" {
		t.Errorf("user %q", u.Name())
	}
	if token := saved(t); token.AccessToken != "a1" || token.RefreshToken != "r1" || time.Until(token.Expiry) < 30*24*time.Hour {
		t.Errorf("saved %+v", token)
	}
	if fake.form.Get("client_id") != "test-id" || fake.form.Get("code_verifier") == "" {
		t.Errorf("exchange form %v", fake.form)
	}
	login, found, err := SavedLogin()
	if err != nil || !found || login.User != "me" || login.Expired() {
		t.Fatalf("saved login: %+v, %v, %v", login, found, err)
	}
}

func TestEnsureLoginRefreshesSoonToExpire(t *testing.T) {
	start(t)
	fake.reset([]string{"old"}, map[string]string{"r0": "new"})
	save(t, oauth2.Token{AccessToken: "old", RefreshToken: "r0", Expiry: time.Now().Add(24 * time.Hour)})

	if err := EnsureLogin(context.Background()); err != nil {
		t.Fatal(err)
	}
	m, _ := client()
	if !m.Authenticated() {
		t.Fatal("not logged in")
	}
	if token := saved(t); token.AccessToken != "new" || token.RefreshToken != "r-new" {
		t.Errorf("saved %+v", token)
	}
	if fake.form.Get("client_id") != "test-id" {
		t.Errorf("refresh with the client %q, want the saved one", fake.form.Get("client_id"))
	}
}

func TestEnsureLoginKeepsFreshToken(t *testing.T) {
	start(t)
	fake.reset([]string{"good"}, map[string]string{"r0": "new"})
	save(t, oauth2.Token{AccessToken: "good", RefreshToken: "r0", Expiry: time.Now().Add(30 * 24 * time.Hour)})
	if err := EnsureLogin(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fake.requests) != 0 || saved(t).AccessToken != "good" {
		t.Errorf("refreshed a fresh token: %v", fake.requests)
	}
}

func TestEnsureLoginRefreshesRejected(t *testing.T) {
	start(t)
	fake.reset(nil, map[string]string{"r0": "new"})
	save(t, oauth2.Token{AccessToken: "old", RefreshToken: "r0", Expiry: time.Now().Add(30 * 24 * time.Hour)})
	if err := EnsureLogin(context.Background()); err != nil {
		t.Fatal(err)
	}
	if token := saved(t); token.AccessToken != "new" {
		t.Errorf("saved %+v", token)
	}
}

func TestEnsureLoginExpiresRejectedRefresh(t *testing.T) {
	start(t)
	fake.reset(nil, nil)
	save(t, oauth2.Token{AccessToken: "old", RefreshToken: "r0", Expiry: time.Now().Add(-time.Hour)})

	if err := EnsureLogin(context.Background()); !errors.Is(err, oauth.ErrRefreshRejected) {
		t.Fatalf("got %v", err)
	}
	login, found, err := SavedLogin()
	if err != nil || !found || !login.Expired() || login.Err != nil {
		t.Fatalf("saved login after the rejected refresh: %+v, %v, %v", login, found, err)
	}
	// not tried again
	loginTried = false
	fake.reset(nil, nil)
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
	fake.reset([]string{"x"}, nil)
	save(t, oauth2.Token{AccessToken: "x", RefreshToken: "r", Expiry: time.Now().Add(30 * 24 * time.Hour)})
	if err := EnsureLogin(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := Logout(); err != nil {
		t.Fatal(err)
	}
	m, _ := client()
	if m.Authenticated() {
		t.Error("still logged in")
	}
	if _, found, err := SavedLogin(); err != nil || found {
		t.Fatalf("saved login after the logout: %v, %v", found, err)
	}
	if users, _ := Users(); len(users) != 1 {
		t.Errorf("the user is still saved: %v", users)
	}
	if _, err := LoginSaved(context.Background(), "me"); err != nil {
		t.Fatal(err)
	}
	if _, found, err := SavedLogin(); err != nil || !found {
		t.Fatalf("saved login after logging in again: %v, %v", found, err)
	}

	if err := Forget("me"); err != nil {
		t.Fatal(err)
	}
	if users, _ := Users(); len(users) != 0 {
		t.Errorf("forgot, still saved: %v", users)
	}
	if _, err := LoginSaved(context.Background(), "me"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("login saved after forget: %v", err)
	}
}

func TestSearchCapsTheQuery(t *testing.T) {
	start(t)
	fake.reset(nil, nil)
	p, err := MyAnimeList()
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("ñ", 50) + " " + strings.Repeat("b", 50)
	if _, err := p.SearchContext(context.Background(), long); err != nil {
		t.Fatal(err)
	}
	if got := []rune(fake.query); len(got) > 64 || !strings.HasPrefix(long, fake.query) {
		t.Errorf("query %q (%d runes)", fake.query, len(got))
	}
}

func TestNotConfigured(t *testing.T) {
	malLock.Lock()
	kept := provider
	provider = nil
	malLock.Unlock()
	defer func() {
		malLock.Lock()
		provider = kept
		malLock.Unlock()
		_ = cache.SetMyAnimeListClient("test-id", "")
	}()
	if !Configured() {
		t.Error("not configured with a saved client id")
	}
	if err := cache.SetMyAnimeListClient("", ""); err != nil {
		t.Fatal(err)
	}
	if Configured() {
		t.Error("configured without a client id")
	}
	if _, err := MyAnimeList(); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("got %v, want ErrNotConfigured", err)
	}
	if err := EnsureLogin(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("ensure login: %v", err)
	}
	if _, err := LoginNew(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("login new: %v", err)
	}
	if err := Logout(); err != nil {
		t.Errorf("logout: %v", err)
	}
}

func TestInvalidClientIDSearchesOnce(t *testing.T) {
	start(t)
	fake.reset(nil, nil)
	fake.badClient = true
	p, err := MyAnimeList()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.SearchContext(context.Background(), "one"); !errors.Is(err, myanimelist.ErrInvalidClientID) {
		t.Fatalf("first search: %v", err)
	}
	if res, err := p.SearchContext(context.Background(), "two"); err != nil || len(res) != 0 {
		t.Fatalf("second search: %v, %v", res, err)
	}
	if fake.searches != 1 {
		t.Errorf("%d requests, the refused id is asked once", fake.searches)
	}
}

func TestSavedClientChangeRebuilds(t *testing.T) {
	first, err := MyAnimeList()
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.SetMyAnimeListClient("other-id", "other-secret"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cache.SetMyAnimeListClient("test-id", "") }()
	second, err := MyAnimeList()
	if err != nil {
		t.Fatal(err)
	}
	if second == first || providerID != "other-id" {
		t.Errorf("provider not rebuilt for the new client id (%q)", providerID)
	}
}

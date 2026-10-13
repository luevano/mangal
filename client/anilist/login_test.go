package anilist

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adrg/xdg"
	"github.com/luevano/mangal/v5/auth/oauth"
	"github.com/luevano/mangal/v5/util/cache"
	"golang.org/x/oauth2"
)

func TestMain(m *testing.M) {
	// keep the logins away from the user's databases
	dir, err := os.MkdirTemp("", "mangal-anilist-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("MANGAL_CACHE_PATH", filepath.Join(dir, "cache"))
	os.Setenv("MANGAL_AUTH_KEYRING", "false")
	xdg.DataHome = filepath.Join(dir, "data")
	httpClient = func() *http.Client { return &http.Client{Transport: fakeViewer{}} }
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestLogoutSticks(t *testing.T) {
	token := &oauth2.Token{AccessToken: "x", Expiry: time.Now().Add(time.Hour)}
	if err := cache.SetAnilistAuthData("me", token); err != nil {
		t.Fatal(err)
	}
	if err := account.UpdateUsers(func(h *cache.UserHistory) { h.Add("me") }); err != nil {
		t.Fatal(err)
	}
	if _, found, err := SavedLogin(); err != nil || !found {
		t.Fatalf("saved login before the logout: %v, %v", found, err)
	}

	if err := Logout(); err != nil {
		t.Fatal(err)
	}
	// what the next run would log in with
	if _, found, err := SavedLogin(); err != nil || found {
		t.Fatalf("saved login after the logout: %v, %v", found, err)
	}
	if users, _ := Users(); len(users) != 1 {
		t.Errorf("the user is still saved: %v", users)
	}
	if notice := LoginNotice(); notice != "" {
		t.Errorf("notice after the logout: %q", notice)
	}

	// a login (here as LoginSaved/LoginNew do after logging in) undoes it
	if err := cache.SetAnilistLoggedOut(false); err != nil {
		t.Fatal(err)
	}
	if _, found, err := SavedLogin(); err != nil || !found {
		t.Fatalf("saved login after logging in again: %v, %v", found, err)
	}
}

// fakeViewer answers every AniList API request with the user "me".
type fakeViewer struct{}

// A lookup by ids (MetadataByIDsBatch) gets the mangas of the ids below 100000, titled
// "manga <id>", and is counted in idRequests.
func (fakeViewer) RoundTrip(req *http.Request) (*http.Response, error) {
	body := `{"data":{"Viewer":{"id":1,"name":"me"}}}`
	var sent struct {
		Variables struct {
			IDs []int `json:"ids"`
		} `json:"variables"`
	}
	if req.Body != nil {
		_ = json.NewDecoder(req.Body).Decode(&sent)
	}
	if ids := sent.Variables.IDs; len(ids) != 0 {
		idRequests.Add(1)
		var media []string
		for _, id := range ids {
			if id < 100000 {
				media = append(media, fmt.Sprintf(`{"id":%d,"title":{"romaji":"manga %d"}}`, id, id))
			}
		}
		body = `{"data":{"Page":{"media":[` + strings.Join(media, ",") + `]}}}`
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

var idRequests atomic.Int32

func TestLoginNewSavesClient(t *testing.T) {
	pasted := make(chan string, 1)
	u, err := LoginManual(context.Background(), " the-id ", "", oauth.Manual{
		Pasted: pasted,
		Show: func(authURL string) {
			u, _ := url.Parse(authURL)
			state := u.Query().Get("state")
			pasted <- oauth.AnilistServerCallbackURL + "#access_token=tok&token_type=Bearer&expires_in=3600&state=" + state
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if u.Name() != "me" {
		t.Errorf("user %q", u.Name())
	}
	if id, secret, err := Client(); err != nil || id != "the-id" || secret != "" {
		t.Errorf("saved client %q %q, %v", id, secret, err)
	}
}

// TestResetLoginDropsTheSession: a logout elsewhere (a TUI while serve runs) logs this
// process out on the next EnsureLogin.
func TestResetLoginDropsTheSession(t *testing.T) {
	token := &oauth2.Token{AccessToken: "x", Expiry: time.Now().Add(time.Hour)}
	if err := cache.SetAnilistAuthData("me", token); err != nil {
		t.Fatal(err)
	}
	if err := account.UpdateUsers(func(h *cache.UserHistory) { h.Add("me") }); err != nil {
		t.Fatal(err)
	}
	if err := cache.SetAnilistLoggedOut(false); err != nil {
		t.Fatal(err)
	}
	ResetLogin()
	if err := EnsureLogin(context.Background()); err != nil {
		t.Fatal(err)
	}
	ani, _ := Anilist()
	if !ani.Authenticated() {
		t.Fatal("not logged in")
	}

	if err := cache.SetAnilistLoggedOut(true); err != nil {
		t.Fatal(err)
	}
	ResetLogin()
	if err := EnsureLogin(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ani.Authenticated() {
		t.Error("still logged in after the logout elsewhere")
	}
}

package oauth

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
)

// malTokens is a fake MyAnimeList token endpoint that wants the code and its PKCE verifier.
type malTokens struct {
	*httptest.Server
	mu       sync.Mutex
	verifier string
	form     url.Values
}

func newMALTokens(t *testing.T) *malTokens {
	m := &malTokens{}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		m.mu.Lock()
		m.form = r.PostForm
		want := m.verifier
		m.mu.Unlock()
		switch {
		case r.PostForm.Get("grant_type") == "refresh_token" && r.PostForm.Get("refresh_token") == "good-refresh":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"refreshed","refresh_token":"new-refresh","token_type":"Bearer","expires_in":2678400}`))
		case r.PostForm.Get("code") == "the-code" && want != "" && r.PostForm.Get("code_verifier") == want:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"exchanged","refresh_token":"refresh","token_type":"Bearer","expires_in":2678400}`))
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		}
	}))
	endpoint := MyAnimeListEndpoint
	MyAnimeListEndpoint.TokenURL = m.URL
	t.Cleanup(func() {
		MyAnimeListEndpoint = endpoint
		m.Close()
	})
	return m
}

func testMALLogin(t *testing.T, browse func(base string)) (*MyAnimeListLoginOption, *net.Listener) {
	t.Helper()
	o, err := NewMyAnimeListLoginOption("id", "")
	if err != nil {
		t.Fatal(err)
	}
	var ln net.Listener
	o.server.listen = func() (net.Listener, error) {
		var err error
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		return ln, err
	}
	o.server.openURL = func(string) error {
		go browse("http://" + ln.Addr().String())
		return nil
	}
	return o, &ln
}

// malAuthURL follows the login page to MyAnimeList's url.
func malAuthURL(t *testing.T, base string) url.Values {
	res, err := noRedirects.Get(base + myAnimeListLoginPath)
	if err != nil {
		t.Error(err)
		return nil
	}
	res.Body.Close()
	u, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		t.Error(err)
		return nil
	}
	return u.Query()
}

func TestMyAnimeListCodeGrant(t *testing.T) {
	tokens := newMALTokens(t)
	o, ln := testMALLogin(t, func(base string) {
		q := malAuthURL(t, base)
		if q.Get("code_challenge_method") != "plain" || q.Get("response_type") != "code" || q.Get("client_id") != "id" {
			t.Errorf("authorize url %v", q)
		}
		challenge := q.Get("code_challenge")
		if len(challenge) < 43 || len(challenge) > 128 {
			t.Errorf("code challenge %q, MAL wants 43 to 128 chars", challenge)
		}
		tokens.mu.Lock()
		tokens.verifier = challenge
		tokens.mu.Unlock()
		cb := base + myAnimeListCallbackPath
		if code := get(t, cb+"?state=other&code=evil"); code != http.StatusBadRequest {
			t.Errorf("wrong state: %d", code)
		}
		if code := get(t, cb+"?state="+q.Get("state")+"&code=the-code"); code != http.StatusOK {
			t.Errorf("callback: %d", code)
		}
	})
	if err := o.Authorize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if o.Token().AccessToken != "exchanged" || o.Token().RefreshToken != "refresh" {
		t.Errorf("token %+v", o.Token())
	}
	tokens.mu.Lock()
	if tokens.form.Get("code_verifier") != tokens.verifier || tokens.form.Get("redirect_uri") != MyAnimeListServerCallbackURL {
		t.Errorf("exchange form %v", tokens.form)
	}
	if _, ok := tokens.form["client_secret"]; ok {
		t.Error("client_secret sent without one")
	}
	tokens.mu.Unlock()
	if conn, err := net.Dial("tcp", (*ln).Addr().String()); err == nil {
		conn.Close()
		t.Error("callback server still listening")
	}

	// denied in the browser
	o, _ = testMALLogin(t, func(base string) {
		get(t, base+myAnimeListCallbackPath+"?state="+malAuthURL(t, base).Get("state")+"&error=access_denied")
	})
	if err := o.Authorize(context.Background()); err == nil {
		t.Error("no error when denied")
	}
}

func TestMyAnimeListManual(t *testing.T) {
	tokens := newMALTokens(t)
	o, err := NewMyAnimeListLoginOption("id", "")
	if err != nil {
		t.Fatal(err)
	}
	// the port is taken: paste only
	o.server.listen = func() (net.Listener, error) { return nil, errors.New("address already in use") }
	o.server.openURL = func(string) error { t.Error("opened the browser"); return nil }

	pasted := make(chan string, 3)
	var bad []error
	manual := Manual{
		Pasted:   pasted,
		BadPaste: func(err error) { bad = append(bad, err) },
		Show: func(authURL string) {
			u, err := url.Parse(authURL)
			if err != nil {
				t.Fatal(err)
			}
			q := u.Query()
			tokens.mu.Lock()
			tokens.verifier = q.Get("code_challenge")
			tokens.mu.Unlock()
			pasted <- "not a url or code"
			pasted <- MyAnimeListServerCallbackURL + "?code=evil&state=other"
			pasted <- MyAnimeListServerCallbackURL + "?code=the-code&state=" + q.Get("state")
		},
	}
	if err := o.AuthorizeManual(context.Background(), manual); err != nil {
		t.Fatal(err)
	}
	if o.Token().AccessToken != "exchanged" {
		t.Errorf("token %+v", o.Token())
	}
	if len(bad) != 2 || !errors.Is(bad[0], ErrBadPaste) || !errors.Is(bad[1], ErrInvalidState) {
		t.Errorf("bad pastes %v", bad)
	}
}

func TestRefreshMyAnimeList(t *testing.T) {
	newMALTokens(t)
	token, err := RefreshMyAnimeList(context.Background(), "id", "", "good-refresh")
	if err != nil {
		t.Fatal(err)
	}
	if token.AccessToken != "refreshed" || token.RefreshToken != "new-refresh" {
		t.Errorf("token %+v", token)
	}
	if _, err := RefreshMyAnimeList(context.Background(), "id", "", "revoked"); !errors.Is(err, ErrRefreshRejected) {
		t.Errorf("got %v, want ErrRefreshRejected", err)
	}
}

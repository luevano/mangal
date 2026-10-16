package oauth

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/luevano/libmangal/metadata/anilist"
)

// testLogin is a login option on a free local port whose "browser" runs browse with the
// server's base url.
func testLogin(t *testing.T, secret string, browse func(base string)) (*AnilistLoginOption, *net.Listener) {
	t.Helper()
	o, err := NewAnilistLoginOption("id", secret)
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

// noKeepAlive keeps the transport from dialing spare connections, the server's shutdown
// waits seconds for those
var noKeepAlive = &http.Transport{DisableKeepAlives: true}

var noRedirects = &http.Client{Transport: noKeepAlive, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// loginState follows the login page to AniList's url and returns its state.
func loginState(t *testing.T, base string) string {
	res, err := noRedirects.Get(base + anilistLoginPath)
	if err != nil {
		t.Error(err)
		return ""
	}
	res.Body.Close()
	u, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		t.Error(err)
		return ""
	}
	return u.Query().Get("state")
}

func get(t *testing.T, u string) int {
	res, err := (&http.Client{Transport: noKeepAlive}).Get(u)
	if err != nil {
		t.Error(err)
		return 0
	}
	res.Body.Close()
	return res.StatusCode
}

func TestImplicitGrant(t *testing.T) {
	o, ln := testLogin(t, "", func(base string) {
		state := loginState(t, base)
		if state == "" {
			t.Error("no state in the login url")
		}
		cb := base + anilistCallbackPath
		// the page that resends the fragment
		if code := get(t, cb); code != http.StatusOK {
			t.Errorf("resend page: %d", code)
		}
		// a request with another state doesn't end the login
		if code := get(t, cb+"?state=other&access_token=evil"); code != http.StatusBadRequest {
			t.Errorf("wrong state: %d", code)
		}
		if code := get(t, cb+"?state="+state+"&access_token=good&token_type=Bearer"); code != http.StatusOK {
			t.Errorf("callback: %d", code)
		}
	})
	if err := o.Authorize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if o.Token() == nil || o.Token().AccessToken != "good" {
		t.Errorf("token %+v", o.Token())
	}
	// the server is shut down
	if conn, err := net.Dial("tcp", (*ln).Addr().String()); err == nil {
		conn.Close()
		t.Error("callback server still listening")
	}
}

func TestCodeGrant(t *testing.T) {
	tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("code") != "the-code" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"exchanged","token_type":"Bearer","expires_in":3600}`))
	}))
	defer tokens.Close()
	endpoint := anilist.OAuthEndpoint
	anilist.OAuthEndpoint.TokenURL = tokens.URL
	defer func() { anilist.OAuthEndpoint = endpoint }()

	o, _ := testLogin(t, "secret", func(base string) {
		state := loginState(t, base)
		get(t, base+anilistCallbackPath+"?state=other&code=evil")
		get(t, base+anilistCallbackPath+"?state="+state+"&code=the-code")
	})
	if err := o.Authorize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if o.Token().AccessToken != "exchanged" {
		t.Errorf("token %+v", o.Token())
	}

	// denied in the browser
	o, _ = testLogin(t, "secret", func(base string) {
		get(t, base+anilistCallbackPath+"?state="+loginState(t, base)+"&error=access_denied")
	})
	if err := o.Authorize(context.Background()); err == nil {
		t.Error("no error when denied")
	}
}

func TestAuthorizeErrors(t *testing.T) {
	// the port is taken
	o, _ := NewAnilistLoginOption("id", "")
	busy := errors.New("address already in use")
	o.server.listen = func() (net.Listener, error) { return nil, busy }
	if err := o.Authorize(context.Background()); !errors.Is(err, busy) {
		t.Errorf("got %v, want the listen error", err)
	}

	// the browser can't be opened: the server still shuts down
	o, ln := testLogin(t, "", nil)
	o.server.openURL = func(string) error { return errors.New("no browser") }
	if err := o.Authorize(context.Background()); err == nil {
		t.Error("no error without a browser")
	}
	if conn, err := net.Dial("tcp", (*ln).Addr().String()); err == nil {
		conn.Close()
		t.Error("callback server still listening")
	}

	// cancelled
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	o, _ = testLogin(t, "", func(string) {})
	if err := o.Authorize(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("got %v, want the ctx error", err)
	}
}

// the pin login: no callback server, AniList's pin page as redirect, the code it shows pasted
func TestAnilistPin(t *testing.T) {
	var redirect string
	tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirect = r.FormValue("redirect_uri")
		if r.FormValue("code") != "pin-code" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"from-pin","token_type":"Bearer","expires_in":3600}`))
	}))
	defer tokens.Close()
	endpoint := anilist.OAuthEndpoint
	anilist.OAuthEndpoint.TokenURL = tokens.URL
	defer func() { anilist.OAuthEndpoint = endpoint }()

	login := func(secret, paste string) (*AnilistLoginOption, string, error) {
		o, err := NewAnilistLoginOption("id", secret)
		if err != nil {
			t.Fatal(err)
		}
		o.Pin = true
		o.server.listen = func() (net.Listener, error) {
			t.Error("callback server started")
			return nil, errors.New("no")
		}
		o.server.openURL = func(string) error { t.Error("browser opened"); return nil }
		pasted := make(chan string, 1)
		var shown string
		err = o.AuthorizeManual(context.Background(), Manual{Pasted: pasted, Show: func(u string) { shown = u; pasted <- paste }})
		return o, shown, err
	}

	o, shown, err := login("secret", " pin-code \n")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(shown)
	if u.Query().Get("redirect_uri") != anilist.OAuthPinURL || redirect != anilist.OAuthPinURL || o.Token().AccessToken != "from-pin" {
		t.Errorf("shown %q, exchanged with redirect %q, token %+v", shown, redirect, o.Token())
	}
	if o.RedirectURL() != anilist.OAuthPinURL {
		t.Errorf("redirect url %q", o.RedirectURL())
	}

	// implicit: the page shows the token, the client's redirect url is used
	o, shown, err = login("", "the-token")
	if err != nil || o.Token().AccessToken != "the-token" {
		t.Fatalf("implicit: %v, %+v", err, o.Token())
	}
	if u, _ := url.Parse(shown); u.Query().Get("response_type") != "token" {
		t.Errorf("implicit url %q", shown)
	}

	// only a paste can finish it
	o, _ = NewAnilistLoginOption("id", "")
	o.Pin = true
	if err := o.Authorize(context.Background()); !errors.Is(err, ErrNeedsPaste) {
		t.Errorf("without a paste: %v", err)
	}
}

// a busy port: told, the paste still finishes the login
func TestManualNoServer(t *testing.T) {
	o, _ := NewAnilistLoginOption("id", "")
	busy := errors.New("address already in use")
	o.server.listen = func() (net.Listener, error) { return nil, busy }
	pasted := make(chan string, 1)
	var told error
	err := o.AuthorizeManual(context.Background(), Manual{
		Pasted:   pasted,
		NoServer: func(err error) { told = err },
		Show:     func(string) { pasted <- "#access_token=tok" },
	})
	if err != nil || o.Token().AccessToken != "tok" || !errors.Is(told, busy) {
		t.Errorf("got %v, token %+v, told %v", err, o.Token(), told)
	}
}

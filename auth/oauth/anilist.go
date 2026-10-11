// Package oauth logs in to AniList with OAuth2, through a local callback server.
package oauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/luevano/libmangal/metadata/anilist"
	"github.com/skratchdot/open-golang/open"
	"golang.org/x/oauth2"
)

// Reference docs:
// https://anilist.gitbook.io/anilist-apiv2-docs/overview/oauth/getting-started

const (
	// AnilistServerAddr is where the callback server listens, only reachable from this
	// machine. The AniList API client must have AnilistServerCallbackURL as redirect URL.
	AnilistServerAddr        = "127.0.0.1:6969"
	AnilistServerBaseURL     = "http://localhost:6969/oauth/al/"
	AnilistServerLoginURL    = AnilistServerBaseURL + "login"
	AnilistServerCallbackURL = AnilistServerBaseURL + "callback"

	anilistLoginPath    = "/oauth/al/login"
	anilistCallbackPath = "/oauth/al/callback"

	// loginTimeout is how long the callback server waits for the login.
	loginTimeout = time.Minute
)

var AnilistEndpoint = oauth2.Endpoint{
	AuthURL:   anilist.OAuthAuthorizeURL,
	TokenURL:  anilist.OAuthTokenURL,
	AuthStyle: oauth2.AuthStyleInParams,
}

// AnilistLoginOption logs in to AniList with OAuth2.
//
// WARN: the implicit grant (empty ClientSecret) should be used with caution and only if
// completely needed, due to the access token being resent (to the same callback URL) on
// the URL params. AniList sends it in the URL fragment specifically so that servers
// don't see the token.
type AnilistLoginOption struct {
	// ClientID is the application's ID.
	//
	// Must be non-empty.
	ClientID string

	// ClientSecret is the application's secret.
	//
	// When empty, the OAuth is an implicit grant.
	ClientSecret string

	token *oauth2.Token

	// listen and openURL are replaced in tests.
	listen  func() (net.Listener, error)
	openURL func(string) error
}

func NewAnilistLoginOption(clientID, clientSecret string) (*AnilistLoginOption, error) {
	if clientID == "" {
		return nil, ErrEmptyClientID
	}

	return &AnilistLoginOption{
		ClientID:     clientID,
		ClientSecret: clientSecret,
	}, nil
}

func (o *AnilistLoginOption) getOAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     o.ClientID,
		ClientSecret: o.ClientSecret,
		Endpoint:     AnilistEndpoint,
		RedirectURL:  AnilistServerCallbackURL,
	}
}

// getImplicitGrantURL is the implicit grant url: response_type token and no redirect uri.
func (o *AnilistLoginOption) getImplicitGrantURL(state string) string {
	var buf bytes.Buffer
	c := o.getOAuthConfig()
	buf.WriteString(c.Endpoint.AuthURL)
	v := url.Values{
		"response_type": {"token"},
		"client_id":     {o.ClientID},
		"state":         {state},
	}
	if strings.Contains(c.Endpoint.AuthURL, "?") {
		buf.WriteByte('&')
	} else {
		buf.WriteByte('?')
	}
	buf.WriteString(v.Encode())
	return buf.String()
}

// String the name of the login option, for logging purposes.
func (o *AnilistLoginOption) String() string {
	return "Anilist OAuth2"
}

// result is the outcome of the login, sent once by the callback handler.
type result struct {
	token *oauth2.Token
	err   error
}

// Authorize obtains the oauth2 Token:
//
// 1. It starts an http server on AnilistServerAddr to get AniList's callback with the
// access code (or token), and opens the login page in the browser. For more:
// https://anilist.gitbook.io/anilist-apiv2-docs/overview/oauth/getting-started#auth-pin
//
// 2. On a code grant (ClientSecret not empty), it converts the access code to an access
// token. For more:
// https://anilist.gitbook.io/anilist-apiv2-docs/overview/oauth/authorization-code-grant#converting-authorization-codes-to-access-tokens
//
// The server lives until the token is retrieved, ctx is done or a minute passes.
func (o *AnilistLoginOption) Authorize(ctx context.Context) error {
	if o.ClientID == "" {
		return ErrEmptyClientID
	}
	state, err := randomState()
	if err != nil {
		return err
	}

	listen := o.listen
	if listen == nil {
		listen = func() (net.Listener, error) { return net.Listen("tcp", AnilistServerAddr) }
	}
	ln, err := listen()
	if err != nil {
		return fmt.Errorf("start the oauth callback server: %w", err)
	}

	results := make(chan result, 1)
	var once sync.Once
	done := func(r result) {
		once.Do(func() { results <- r })
	}
	var handler http.Handler
	if o.ClientSecret != "" {
		handler = o.codeGrantHandler(state, done)
	} else {
		handler = o.implicitGrantHandler(state, done)
	}

	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	openURL := o.openURL
	if openURL == nil {
		openURL = open.Start
	}
	if err := openURL(AnilistServerLoginURL); err != nil {
		return fmt.Errorf("open %s: %w", AnilistServerLoginURL, err)
	}

	timeout := time.NewTimer(loginTimeout)
	defer timeout.Stop()
	select {
	case r := <-results:
		if r.err != nil {
			return r.err
		}
		if r.token == nil {
			return ErrNilToken
		}
		o.token = r.token
		return nil
	case err := <-serveErr:
		return fmt.Errorf("oauth callback server: %w", err)
	case <-timeout.C:
		return ErrTimeout
	case <-ctx.Done():
		return ctx.Err()
	}
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("oauth state: %w", err)
	}
	return hex.EncodeToString(b), nil
}

const closeMsg = "; this window can now be closed"

// fail answers the browser and ends the login with the error.
func fail(w http.ResponseWriter, done func(result), err error) {
	http.Error(w, "error: "+err.Error()+closeMsg, http.StatusBadRequest)
	done(result{err: err})
}

// codeGrantHandler handles the login and callback of a code grant (ClientSecret not empty).
func (o *AnilistLoginOption) codeGrantHandler(state string, done func(result)) http.Handler {
	oAuthConfig := o.getOAuthConfig()
	var mu sync.Mutex
	called := false

	mux := http.NewServeMux()
	mux.HandleFunc(anilistLoginPath, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, oAuthConfig.AuthCodeURL(state), http.StatusTemporaryRedirect)
	})
	mux.HandleFunc(anilistCallbackPath, func(w http.ResponseWriter, r *http.Request) {
		// a request that isn't AniList's answer to this login doesn't end it
		if r.FormValue("state") != state {
			http.Error(w, "error: "+ErrInvalidState.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		again := called
		called = true
		mu.Unlock()
		if again {
			http.Error(w, "error: "+ErrTooManyCallbacks.Error()+closeMsg, http.StatusBadRequest)
			return
		}

		if e := r.FormValue("error"); e != "" {
			fail(w, done, fmt.Errorf("oauth: %s", e))
			return
		}
		code := r.FormValue("code")
		if code == "" {
			fail(w, done, ErrEmptyCode)
			return
		}
		token, err := oAuthConfig.Exchange(r.Context(), code)
		if err != nil {
			fail(w, done, fmt.Errorf("exchange the access code: %w", err))
			return
		}
		_, _ = w.Write([]byte("successfully got access token (code grant)" + closeMsg))
		done(result{token: token})
	})
	return mux
}

// implicitGrantHandler handles the login and callback of an implicit grant (ClientSecret
// empty).
func (o *AnilistLoginOption) implicitGrantHandler(state string, done func(result)) http.Handler {
	var mu sync.Mutex
	called := false

	mux := http.NewServeMux()
	mux.HandleFunc(anilistLoginPath, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, o.getImplicitGrantURL(state), http.StatusTemporaryRedirect)
	})
	mux.HandleFunc(anilistCallbackPath, func(w http.ResponseWriter, r *http.Request) {
		// the token comes in the url fragment, which only the browser sees: a page resends
		// it as url params
		if r.URL.RawQuery == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, resendFragmentAsParam)
			return
		}
		if r.FormValue("state") != state {
			http.Error(w, "error: "+ErrInvalidState.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		again := called
		called = true
		mu.Unlock()
		if again {
			http.Error(w, "error: "+ErrTooManyCallbacks.Error()+closeMsg, http.StatusBadRequest)
			return
		}

		if e := r.FormValue("error"); e != "" {
			fail(w, done, fmt.Errorf("oauth: %s", e))
			return
		}
		accessToken := r.FormValue("access_token")
		if accessToken == "" {
			fail(w, done, ErrEmptyCode)
			return
		}
		// AniList's implicit grant tokens last a year
		token := &oauth2.Token{
			AccessToken: accessToken,
			TokenType:   "Bearer",
			Expiry:      time.Now().Add(365 * 24 * time.Hour),
		}
		_, _ = w.Write([]byte("successfully got access token (implicit grant)" + closeMsg))
		done(result{token: token})
	})
	return mux
}

// Token returns the authorization token (useful for caching).
func (o *AnilistLoginOption) Token() *oauth2.Token {
	return o.token
}

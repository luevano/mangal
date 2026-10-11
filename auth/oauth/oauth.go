// Package oauth logs in to AniList and MyAnimeList with OAuth2, through a local callback
// server.
package oauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/skratchdot/open-golang/open"
	"golang.org/x/oauth2"
)

const (
	// ServerAddr is where the callback server listens, only reachable from this machine.
	ServerAddr    = "127.0.0.1:6969"
	serverBaseURL = "http://localhost:6969"

	// loginTimeout is how long the callback server waits for the login in the browser.
	loginTimeout = time.Minute
	// manualTimeout is the same for a manual login, copying the url takes longer.
	manualTimeout = 10 * time.Minute
)

// Manual is a login for when the browser may run on another machine (e.g. over ssh): the
// authorize url is shown instead of opened, and the address the browser ends up on (or the
// code or token in it) can be pasted. The callback server still runs if it can, for a
// browser on this machine.
type Manual struct {
	// Show shows the authorize url to the user. Required.
	Show func(authURL string)
	// Pasted are the pasted answers, nil if none can be pasted.
	Pasted <-chan string
	// BadPaste is told about a paste that can't be used, the login keeps waiting. When nil
	// such a paste ends the login with its error.
	BadPaste func(error)
}

// result is the outcome of the login, sent once by the callback handler.
type result struct {
	token *oauth2.Token
	err   error
}

// flow is a provider's login.
type flow struct {
	// loginURL is the local url that redirects to authURL.
	loginURL string
	authURL  func(state string) string
	// handler serves the login page and the callback, requests made for it use ctx
	handler func(ctx context.Context, state string, done func(result)) http.Handler
	paste   func(ctx context.Context, state, input string) (*oauth2.Token, error)
}

// server is how the login reaches the network and the browser, replaced in tests.
type server struct {
	listen  func() (net.Listener, error)
	openURL func(string) error
}

func (s server) listener() (net.Listener, error) {
	if s.listen != nil {
		return s.listen()
	}
	return net.Listen("tcp", ServerAddr)
}

func (s server) open(u string) error {
	if s.openURL != nil {
		return s.openURL(u)
	}
	return open.Start(u)
}

// authorize runs the login: the callback server lives until the token is retrieved, ctx is
// done or the timeout passes. Without manual the login page is opened in the browser.
func authorize(ctx context.Context, f flow, s server, manual *Manual) (*oauth2.Token, error) {
	state, err := randomState()
	if err != nil {
		return nil, err
	}

	results := make(chan result, 1)
	var once sync.Once
	done := func(r result) {
		once.Do(func() { results <- r })
	}

	// nil channels block: no server or nothing to paste never answers
	var serveErr chan error
	ln, err := s.listener()
	switch {
	case err == nil:
		srv := &http.Server{Handler: f.handler(ctx, state, done), ReadHeaderTimeout: 10 * time.Second}
		serveErr = make(chan error, 1)
		go func() { serveErr <- srv.Serve(ln) }()
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutdownCtx)
		}()
	case manual == nil || manual.Pasted == nil:
		return nil, fmt.Errorf("start the oauth callback server: %w", err)
	}

	timeout := loginTimeout
	var pasted <-chan string
	if manual == nil {
		if err := s.open(f.loginURL); err != nil {
			return nil, fmt.Errorf("open %s: %w", f.loginURL, err)
		}
	} else {
		timeout, pasted = manualTimeout, manual.Pasted
		manual.Show(f.authURL(state))
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case r := <-results:
			if r.err != nil {
				return nil, r.err
			}
			if r.token == nil {
				return nil, ErrNilToken
			}
			return r.token, nil
		case input, ok := <-pasted:
			if !ok {
				pasted = nil
				continue
			}
			token, err := f.paste(ctx, state, input)
			if err != nil && manual.BadPaste != nil && ctx.Err() == nil {
				manual.BadPaste(err)
				continue
			}
			return token, err
		case err := <-serveErr:
			return nil, fmt.Errorf("oauth callback server: %w", err)
		case <-timer.C:
			return nil, ErrTimeout
		case <-ctx.Done():
			return nil, ctx.Err()
		}
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

// callback handles the provider's redirect: a request without the login's state doesn't end
// the login, a second one is refused.
func callback(state string, handle func(w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
	var mu sync.Mutex
	called := false
	return func(w http.ResponseWriter, r *http.Request) {
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
		handle(w, r)
	}
}

// codeGrantHandler handles the login page and the callback of a code grant.
func codeGrantHandler(ctx context.Context, loginPath, callbackPath, authURL string, exchange func(ctx context.Context, code string) (*oauth2.Token, error), state string, done func(result)) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(loginPath, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
	})
	mux.HandleFunc(callbackPath, callback(state, func(w http.ResponseWriter, r *http.Request) {
		if e := r.FormValue("error"); e != "" {
			fail(w, done, providerError(e, r.FormValue("error_description")))
			return
		}
		code := r.FormValue("code")
		if code == "" {
			fail(w, done, ErrEmptyCode)
			return
		}
		token, err := exchange(ctx, code)
		if err != nil {
			fail(w, done, err)
			return
		}
		_, _ = w.Write([]byte("successfully got access token" + closeMsg))
		done(result{token: token})
	}))
	return mux
}

func providerError(e, description string) error {
	if description != "" {
		return fmt.Errorf("oauth: %s: %s", e, description)
	}
	return fmt.Errorf("oauth: %s", e)
}

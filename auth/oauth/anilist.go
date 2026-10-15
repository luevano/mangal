package oauth

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/luevano/libmangal/metadata/anilist"
	"golang.org/x/oauth2"
)

const (
	// AnilistServerAddr is where the callback server listens. The AniList API client must
	// have AnilistServerCallbackURL as redirect URL.
	AnilistServerAddr        = ServerAddr
	AnilistServerBaseURL     = serverBaseURL + "/oauth/al/"
	AnilistServerLoginURL    = AnilistServerBaseURL + "login"
	AnilistServerCallbackURL = AnilistServerBaseURL + "callback"

	anilistLoginPath    = "/oauth/al/login"
	anilistCallbackPath = "/oauth/al/callback"
)

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

	server server
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
	return anilist.OAuthConfig(o.ClientID, o.ClientSecret, AnilistServerCallbackURL)
}

func (o *AnilistLoginOption) implicit() bool {
	return o.ClientSecret == ""
}

func (o *AnilistLoginOption) authURL(state string) string {
	if o.implicit() {
		return anilist.ImplicitGrantURL(o.ClientID, state)
	}
	return o.getOAuthConfig().AuthCodeURL(state)
}

// String the name of the login option, for logging purposes.
func (o *AnilistLoginOption) String() string {
	return "Anilist OAuth2"
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
	return o.authorize(ctx, nil)
}

// AuthorizeManual is Authorize showing the authorize url instead of opening it, and
// taking the pasted address the browser ends up on (see Manual). An implicit grant's
// address has the token in its fragment (#access_token=), a code grant's the code.
func (o *AnilistLoginOption) AuthorizeManual(ctx context.Context, manual Manual) error {
	return o.authorize(ctx, &manual)
}

func (o *AnilistLoginOption) authorize(ctx context.Context, manual *Manual) error {
	if o.ClientID == "" {
		return ErrEmptyClientID
	}
	token, err := authorize(ctx, flow{
		loginURL: AnilistServerLoginURL,
		authURL:  o.authURL,
		handler:  o.handler,
		paste:    o.paste,
	}, o.server, manual)
	if err != nil {
		return err
	}
	o.token = token
	return nil
}

func (o *AnilistLoginOption) handler(ctx context.Context, state string, done func(result)) http.Handler {
	if o.implicit() {
		return o.implicitGrantHandler(state, done)
	}
	return codeGrantHandler(ctx, anilistLoginPath, anilistCallbackPath, o.authURL(state), o.exchange, state, done)
}

func (o *AnilistLoginOption) exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return anilist.ExchangeCodeContext(ctx, o.getOAuthConfig(), code)
}

func (o *AnilistLoginOption) paste(ctx context.Context, state, input string) (*oauth2.Token, error) {
	p, err := parsePaste(input, state, o.implicit())
	if err != nil {
		return nil, err
	}
	if o.implicit() {
		return anilist.ImplicitToken(p.token, p.expiresIn), nil
	}
	return o.exchange(ctx, p.code)
}

// implicitGrantHandler handles the login and callback of an implicit grant (ClientSecret
// empty).
func (o *AnilistLoginOption) implicitGrantHandler(state string, done func(result)) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(anilistLoginPath, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, anilist.ImplicitGrantURL(o.ClientID, state), http.StatusTemporaryRedirect)
	})
	handle := callback(state, func(w http.ResponseWriter, r *http.Request) {
		if e := r.FormValue("error"); e != "" {
			fail(w, done, providerError(e, r.FormValue("error_description")))
			return
		}
		accessToken := r.FormValue("access_token")
		if accessToken == "" {
			fail(w, done, ErrEmptyCode)
			return
		}
		var expiresIn time.Duration
		if secs, err := strconv.Atoi(r.FormValue("expires_in")); err == nil {
			expiresIn = time.Duration(secs) * time.Second
		}
		_, _ = w.Write([]byte("successfully got access token (implicit grant)" + closeMsg))
		done(result{token: anilist.ImplicitToken(accessToken, expiresIn)})
	})
	mux.HandleFunc(anilistCallbackPath, func(w http.ResponseWriter, r *http.Request) {
		// the token comes in the url fragment, which only the browser sees: a page resends
		// it as url params
		if r.URL.RawQuery == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, resendFragmentAsParam)
			return
		}
		handle(w, r)
	})
	return mux
}

// Token returns the authorization token (useful for caching).
func (o *AnilistLoginOption) Token() *oauth2.Token {
	return o.token
}

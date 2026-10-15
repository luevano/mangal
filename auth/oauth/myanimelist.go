package oauth

import (
	"context"
	"net/http"

	"github.com/luevano/libmangal/metadata/myanimelist"
	"golang.org/x/oauth2"
)

const (
	// MyAnimeListServerCallbackURL must be the App Redirect URL of the MyAnimeList API client.
	MyAnimeListServerBaseURL     = serverBaseURL + "/oauth/mal/"
	MyAnimeListServerLoginURL    = MyAnimeListServerBaseURL + "login"
	MyAnimeListServerCallbackURL = MyAnimeListServerBaseURL + "callback"

	myAnimeListLoginPath    = "/oauth/mal/login"
	myAnimeListCallbackPath = "/oauth/mal/callback"
)

// MyAnimeListLoginOption logs in to MyAnimeList with an OAuth2 code grant with PKCE.
// MyAnimeList only supports the plain code challenge (the challenge is the verifier).
type MyAnimeListLoginOption struct {
	// ClientID is the application's ID.
	//
	// Must be non-empty.
	ClientID string

	// ClientSecret is the application's secret, empty for the App Type "other".
	ClientSecret string

	token *oauth2.Token

	server server
	// verifier is the PKCE code verifier of the login, set by authorize.
	verifier string
}

func NewMyAnimeListLoginOption(clientID, clientSecret string) (*MyAnimeListLoginOption, error) {
	if clientID == "" {
		return nil, ErrEmptyClientID
	}
	return &MyAnimeListLoginOption{ClientID: clientID, ClientSecret: clientSecret}, nil
}

func (o *MyAnimeListLoginOption) getOAuthConfig() *oauth2.Config {
	return myanimelist.OAuthConfig(o.ClientID, o.ClientSecret, MyAnimeListServerCallbackURL)
}

// String the name of the login option, for logging purposes.
func (o *MyAnimeListLoginOption) String() string {
	return "MyAnimeList OAuth2"
}

func (o *MyAnimeListLoginOption) authURL(state string) string {
	return myanimelist.AuthCodeURL(o.getOAuthConfig(), state, o.verifier)
}

// Authorize obtains the oauth2 Token: it starts an http server on ServerAddr to get
// MyAnimeList's callback with the code, opens the login page in the browser and exchanges
// the code (with the PKCE verifier) for the token.
//
// The server lives until the token is retrieved, ctx is done or a minute passes.
func (o *MyAnimeListLoginOption) Authorize(ctx context.Context) error {
	return o.authorize(ctx, nil)
}

// AuthorizeManual is Authorize showing the authorize url instead of opening it, and
// taking the pasted address the browser ends up on, or its code (see Manual).
func (o *MyAnimeListLoginOption) AuthorizeManual(ctx context.Context, manual Manual) error {
	return o.authorize(ctx, &manual)
}

func (o *MyAnimeListLoginOption) authorize(ctx context.Context, manual *Manual) error {
	if o.ClientID == "" {
		return ErrEmptyClientID
	}
	o.verifier = oauth2.GenerateVerifier()
	token, err := authorize(ctx, flow{
		loginURL: MyAnimeListServerLoginURL,
		authURL:  o.authURL,
		handler: func(ctx context.Context, state string, done func(result)) http.Handler {
			return codeGrantHandler(ctx, myAnimeListLoginPath, myAnimeListCallbackPath, o.authURL(state), o.exchange, state, done)
		},
		paste: o.paste,
	}, o.server, manual)
	if err != nil {
		return err
	}
	o.token = token
	return nil
}

func (o *MyAnimeListLoginOption) exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return myanimelist.ExchangeCodeContext(ctx, o.getOAuthConfig(), code, o.verifier)
}

func (o *MyAnimeListLoginOption) paste(ctx context.Context, state, input string) (*oauth2.Token, error) {
	p, err := parsePaste(input, state, false)
	if err != nil {
		return nil, err
	}
	return o.exchange(ctx, p.code)
}

// Token returns the authorization token (useful for caching).
func (o *MyAnimeListLoginOption) Token() *oauth2.Token {
	return o.token
}

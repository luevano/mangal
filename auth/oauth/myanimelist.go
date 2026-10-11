package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/luevano/libmangal/metadata/myanimelist"
	"golang.org/x/oauth2"
)

// Reference docs:
// https://myanimelist.net/apiconfig/references/authorization
// https://myanimelist.net/blog.php?eid=835707

const (
	// MyAnimeListServerCallbackURL must be the App Redirect URL of the MyAnimeList API client.
	MyAnimeListServerBaseURL     = serverBaseURL + "/oauth/mal/"
	MyAnimeListServerLoginURL    = MyAnimeListServerBaseURL + "login"
	MyAnimeListServerCallbackURL = MyAnimeListServerBaseURL + "callback"

	myAnimeListLoginPath    = "/oauth/mal/login"
	myAnimeListCallbackPath = "/oauth/mal/callback"
)

var MyAnimeListEndpoint = oauth2.Endpoint{
	AuthURL:   myanimelist.OAuthAuthorizeURL,
	TokenURL:  myanimelist.OAuthTokenURL,
	AuthStyle: oauth2.AuthStyleInParams,
}

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

func myAnimeListConfig(clientID, clientSecret string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     MyAnimeListEndpoint,
		RedirectURL:  MyAnimeListServerCallbackURL,
	}
}

func (o *MyAnimeListLoginOption) getOAuthConfig() *oauth2.Config {
	return myAnimeListConfig(o.ClientID, o.ClientSecret)
}

// String the name of the login option, for logging purposes.
func (o *MyAnimeListLoginOption) String() string {
	return "MyAnimeList OAuth2"
}

func (o *MyAnimeListLoginOption) authURL(state string) string {
	return o.getOAuthConfig().AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", o.verifier),
		oauth2.SetAuthURLParam("code_challenge_method", "plain"),
	)
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
	token, err := o.getOAuthConfig().Exchange(ctx, code, oauth2.VerifierOption(o.verifier))
	if err != nil {
		return nil, fmt.Errorf("exchange the access code: %w", err)
	}
	return token, nil
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

// ErrRefreshRejected is a refresh token MyAnimeList rejected: the login has to be done again.
var ErrRefreshRejected = errors.New("oauth: refresh token rejected")

// RefreshMyAnimeList gets a new token with the refresh token. The http client used is
// ctx's oauth2.HTTPClient, if any. A rejected refresh token matches ErrRefreshRejected.
func RefreshMyAnimeList(ctx context.Context, clientID, clientSecret, refreshToken string) (*oauth2.Token, error) {
	if clientID == "" {
		return nil, ErrEmptyClientID
	}
	expired := &oauth2.Token{RefreshToken: refreshToken, Expiry: time.Now().Add(-time.Hour)}
	token, err := myAnimeListConfig(clientID, clientSecret).TokenSource(ctx, expired).Token()
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) && re.Response != nil && (re.Response.StatusCode == http.StatusBadRequest || re.Response.StatusCode == http.StatusUnauthorized) {
			return nil, fmt.Errorf("%w: %w", ErrRefreshRejected, err)
		}
		return nil, fmt.Errorf("refresh the token: %w", err)
	}
	return token, nil
}

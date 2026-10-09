package oauth

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/luevano/libmangal/metadata/myanimelist"
	"github.com/luevano/mangal/auth"
	"github.com/skratchdot/open-golang/open"
	"golang.org/x/oauth2"
)

// Reference docs:
// https://myanimelist.net/apiconfig/references/authorization
// https://myanimelist.net/blog.php?eid=835707

const (
	// TODO: make configurable (and used in the oauth handlers)
	MyAnimeListServerBaseRUL     = "http://localhost:6969/oauth/mal/"
	MyAnimeListServerLoginURL    = MyAnimeListServerBaseRUL + "login"
	MyAnimeListServerCallbackURL = MyAnimeListServerBaseRUL + "callback"
)

var MyAnimeListEndpoint = oauth2.Endpoint{
	AuthURL:   myanimelist.OAuthAuthorizeURL,
	TokenURL:  myanimelist.OAuthTokenURL,
	AuthStyle: oauth2.AuthStyleInParams,
}

var _ auth.LoginOption = (*MyAnimeListLoginOption)(nil)

// MyAnimeListLoginOption is an implementation of metadata.LoginOption
// that handles OAuth2 login.
type MyAnimeListLoginOption struct {
	// ClientID is the application's ID.
	//
	// Must be non-empty.
	ClientID string

	// ClientSecret is the application's secret.
	//
	// Could be empty for "Other" App Types.
	ClientSecret string

	// code is the access code
	code  string
	token *oauth2.Token
}

func NewMyAnimeListLoginOption(clientID, clientSecret string) (*MyAnimeListLoginOption, error) {
	if clientID == "" {
		return nil, EmptyClientIDError
	}

	return &MyAnimeListLoginOption{
		ClientID:     clientID,
		ClientSecret: clientSecret,
	}, nil
}

// getOAuthConfig is a convenience method to get the oauth2 Config for
// this OAuthLoginOption.
func (o *MyAnimeListLoginOption) getOAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     o.ClientID,
		ClientSecret: o.ClientSecret,
		Endpoint:     MyAnimeListEndpoint,
		RedirectURL:  MyAnimeListServerCallbackURL, // TODO: make configurable?
	}
}

// String the name of the login option, for logging purposes.
func (o *MyAnimeListLoginOption) String() string {
	return "MyAnimeList OAuth2"
}

// Authorize will perform the authorization steps to obtain the oauth2 Token.
func (o *MyAnimeListLoginOption) Authorize(ctx context.Context) (tokenErr error) {
	if o.ClientID == "" {
		return EmptyClientIDError
	}

	// TODO: make timeout duration configurable?
	//
	// a new ctx is required to close the http server
	// after some time or after receiving the code
	srvCtx, srvCtxCancel := context.WithTimeout(ctx, time.Minute)

	handler := o.codeGrantHandler(srvCtxCancel, &tokenErr)
	if tokenErr != nil {
		return tokenErr
	}

	// start a new http server
	s := &http.Server{
		Addr:    ":6969", // TODO: make configurable
		Handler: handler,
	}
	go func() {
		s.ListenAndServe()
	}()

	// open the web browser on the login url
	if err := open.Start(MyAnimeListServerLoginURL); err != nil {
		return err
	}

	// wait until the server ctx timeout runs out or
	// the access code is retrieved to close the server
	<-srvCtx.Done()
	if err := s.Shutdown(ctx); err != nil {
		return err
	}

	// check if there was an error during
	// the token retrieval or if nil
	if tokenErr != nil {
		return tokenErr
	}
	if o.token == nil {
		return NilTokenError
	}
	return nil
}

// codeGrantHandler provides the http handlers when
// using a code grant oauth (ClientSecret is non-empty)
func (o *MyAnimeListLoginOption) codeGrantHandler(ctxCancel context.CancelFunc, tokenErr *error) http.Handler {
	callbackCount := 0
	closeMsg := "; this window can now be closed"

	oAuthConfig := o.getOAuthConfig()
	state := uuid.New().String()
	codeChallenge := oauth2.GenerateVerifier()

	// TODO: don't hardcode the server paths
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/mal/login", func(w http.ResponseWriter, r *http.Request) {
		u := oAuthConfig.AuthCodeURL(
			state,
			oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		)
		http.Redirect(w, r, u, http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/oauth/mal/callback", func(w http.ResponseWriter, r *http.Request) {
		callbackCount++
		if callbackCount > 1 {
			*tokenErr = TooManyCallbacksError
			w.Write([]byte("error: " + (*tokenErr).Error() + closeMsg))
			ctxCancel()
			return
		}

		if r.FormValue("state") != state {
			*tokenErr = InvalidCallbackStateError
			w.Write([]byte("error: " + (*tokenErr).Error() + closeMsg))
			return
		}

		o.code = r.FormValue("code")
		if o.code == "" {
			*tokenErr = EmptyCodeError
			w.Write([]byte("error: " + (*tokenErr).Error() + closeMsg))
			ctxCancel()
			return
		}

		token, err := oAuthConfig.Exchange(
			r.Context(),
			o.code,
			oauth2.VerifierOption(codeChallenge),
		)
		if err != nil {
			*tokenErr = err
			w.Write([]byte("error: " + (*tokenErr).Error() + closeMsg))
			ctxCancel()
			return
		}
		o.token = token

		w.Write([]byte("successfully got access token (code grant)" + closeMsg))
		ctxCancel()
	})
	return mux
}

// Token returns the authorization token (useful for caching).
func (o *MyAnimeListLoginOption) Token() *oauth2.Token {
	return o.token
}

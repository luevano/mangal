package oauth

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/luevano/libmangal/metadata/anilist"
	"github.com/luevano/mangal/auth"
	"github.com/skratchdot/open-golang/open"
	"golang.org/x/oauth2"
)

// Reference docs:
// https://anilist.gitbook.io/anilist-apiv2-docs/overview/oauth/getting-started

const (
	// TODO: make configurable (and used in the oauth handlers)
	AnilistServerBaseRUL     = "http://localhost:6969/oauth/al/"
	AnilistServerLoginURL    = AnilistServerBaseRUL + "login"
	AnilistServerCallbackURL = AnilistServerBaseRUL + "callback"
)

var AnilistEndpoint = oauth2.Endpoint{
	AuthURL:   anilist.OAuthAuthorizeURL,
	TokenURL:  anilist.OAuthTokenURL,
	AuthStyle: oauth2.AuthStyleInParams,
}

var _ auth.LoginOption = (*AnilistLoginOption)(nil)

// WARN: the implicit grant (empty ClientSecret) should be used
// with caution and only if completely needed, due to the access
// token being resent (to the same callback URL) on the URL params.
// Anilist api was designed to send it as part of the URL fragment,
// specifically so that servers didn't have access to the token directly.
//
// AnilistLoginOption is an implementation of metadata.LoginOption
// that handles OAuth2 login.
type AnilistLoginOption struct {
	// ClientID is the application's ID.
	//
	// Must be non-empty.
	ClientID string

	// ClientSecret is the application's secret.
	//
	// When empty, the OAuth is an implicit grant.
	ClientSecret string

	// code is the access code; when the ClientSecret
	// is empty, this is the access token.
	code  string
	token *oauth2.Token
}

func NewAnilistLoginOption(clientID, clientSecret string) (*AnilistLoginOption, error) {
	if clientID == "" {
		return nil, EmptyClientIDError
	}

	return &AnilistLoginOption{
		ClientID:     clientID,
		ClientSecret: clientSecret,
	}, nil
}

// getOAuthConfig is a convenience method to get the oauth2 Config for
// this OAuthLoginOption.
func (o *AnilistLoginOption) getOAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     o.ClientID,
		ClientSecret: o.ClientSecret,
		Endpoint:     AnilistEndpoint,
		RedirectURL:  AnilistServerCallbackURL, // TODO: make configurable?
	}
}

// getRequestBody is a convenience method to get the OAuthLoginOption
// as a code grant request body.
//
// Only needed when there is a ClientSecret.
func (o *AnilistLoginOption) getRequestBody() map[string]string {
	return map[string]string{
		"client_id":     o.ClientID,
		"client_secret": o.ClientSecret,
		"code":          o.code,
		"grant_type":    "authorization_code",
		"redirect_uri":  AnilistServerCallbackURL, // TODO: make configurable?
	}
}

// getImplicitGrantURL is a convenience method to get the implicit grant
// oauth url, as it requires the response_type to be 'token' and not include
// the redirect uri.
func (o *AnilistLoginOption) getImplicitGrantURL() string {
	var buf bytes.Buffer
	c := o.getOAuthConfig()
	buf.WriteString(c.Endpoint.AuthURL)
	v := url.Values{
		"response_type": {"token"}, // for implicit, needs to be token
		"client_id":     {o.ClientID},
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

// Authorize will perform the authorization steps to obtain the oauth2 Token.
//
// Anilist Oauth2 steps performed are:
//
// 1. Starting an http server to handle anilist callbacks with the
// access code/token. For more:
// https://anilist.gitbook.io/anilist-apiv2-docs/overview/oauth/getting-started#auth-pin
//
// 2. If doing a code grant (ClientSecret is not empty), converting
// the acces code to an access token. For more:
// https://anilist.gitbook.io/anilist-apiv2-docs/overview/oauth/authorization-code-grant#converting-authorization-codes-to-access-tokens
//
// 3. Retrieving (or generating in case of implicit grant) the oauth2 Token.
//
// The http server lives for a max duration of 1 minute or until the access
// token is successfully retrieved/built.
func (o *AnilistLoginOption) Authorize(ctx context.Context) (tokenErr error) {
	if o.ClientID == "" {
		return EmptyClientIDError
	}

	// TODO: make timeout duration configurable?
	//
	// a new ctx is required to close the http server
	// after some time or after receiving the code
	srvCtx, srvCtxCancel := context.WithTimeout(ctx, time.Minute)

	// get the correct handler depending on the grant type
	var handler http.Handler
	if o.ClientSecret != "" {
		handler = o.codeGrantHandler(srvCtxCancel, &tokenErr)
	} else {
		handler = o.implicitGrantHandler(srvCtxCancel, &tokenErr)
	}
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
	if err := open.Start(AnilistServerLoginURL); err != nil {
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
func (o *AnilistLoginOption) codeGrantHandler(ctxCancel context.CancelFunc, tokenErr *error) http.Handler {
	callbackCount := 0
	closeMsg := "; this window can now be closed"

	oAuthConfig := o.getOAuthConfig()

	// TODO: don't hardcode the server paths
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/al/login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, oAuthConfig.AuthCodeURL(""), http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/oauth/al/callback", func(w http.ResponseWriter, r *http.Request) {
		callbackCount++
		if callbackCount > 1 {
			*tokenErr = TooManyCallbacksError
			w.Write([]byte("error: " + (*tokenErr).Error() + closeMsg))
			ctxCancel()
			return
		}

		o.code = r.FormValue("code")
		if o.code == "" {
			*tokenErr = EmptyCodeError
			w.Write([]byte("error: " + (*tokenErr).Error() + closeMsg))
			ctxCancel()
			return
		}

		token, err := oAuthConfig.Exchange(r.Context(), o.code)
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

// implicitGrantHandler provides the http handlers when
// using an implicit grant oauth (ClientSecret is empty)
func (o *AnilistLoginOption) implicitGrantHandler(ctxCancel context.CancelFunc, tokenErr *error) http.Handler {
	callbackCount := 0
	closeMsg := "; this window can now be closed"
	// TODO: don't hardcode the server paths
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/al/login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, o.getImplicitGrantURL(), http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/oauth/al/callback", func(w http.ResponseWriter, r *http.Request) {
		// WARN: the token is sent from anilist as part of the url fragment,
		// so to be able to get the access token, the fragment needs to be
		// "intercepted" with javascript, and then resent as part of the url
		// params; this handles that part, it should only run once
		if r.URL.RawQuery == "" {
			callbackCount++
			if callbackCount > 1 {
				*tokenErr = TooManyCallbacksError
				w.Write([]byte("error: " + (*tokenErr).Error() + closeMsg))
				ctxCancel()
				return
			}

			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, resendFragmentAsParam)
			return
		}

		o.code = r.FormValue("access_token")
		if o.code == "" {
			*tokenErr = EmptyCodeError
			w.Write([]byte("error: " + (*tokenErr).Error() + closeMsg))
			ctxCancel()
			return
		}

		// build the default token with the year expiry
		o.token = &oauth2.Token{
			AccessToken: o.code,
			TokenType:   "Bearer",
			Expiry:      time.Now().Add(31536000 * time.Second),
		}
		w.Write([]byte("successfully got access token (implicit grant)" + closeMsg))
		ctxCancel()
		return
	})
	return mux
}

// Token returns the authorization token (useful for caching).
func (o *AnilistLoginOption) Token() *oauth2.Token {
	return o.token
}

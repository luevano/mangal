// Package remote is mangal's client of a `mangal serve` elsewhere: the API v1 client
// (web/apiv1, generated from web/openapi.yaml) with the token, the server's address only,
// the errors as core's, and the core services over it.
package remote

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/meta"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// APIMajor is the major version of the API this client speaks.
const APIMajor = "1"

var (
	// ErrUnauthorized is a server that doesn't take the token (none, a wrong or removed one).
	ErrUnauthorized = errors.New("unauthorized")
	// ErrForbidden is a server refusing what the token's permissions don't allow.
	ErrForbidden = errors.New("forbidden")
)

// Error is an error the server answered, it matches the core errors of its code
// (core.ErrNotFound, ErrInvalid, ErrBusy, ErrNothingToDo) and ErrUnauthorized/ErrForbidden.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

func (e *Error) Is(target error) bool {
	switch {
	case e.Status == http.StatusUnauthorized:
		return target == ErrUnauthorized
	case e.Status == http.StatusForbidden:
		return target == ErrForbidden
	}
	switch e.Code {
	case "not_found", "run_not_found":
		return target == core.ErrNotFound
	case "invalid":
		return target == core.ErrInvalid
	case "busy":
		return target == core.ErrBusy
	case "refused":
		return target == core.ErrRefused
	case "nothing_to_do":
		return target == core.ErrNothingToDo || target == library.ErrNothingToUndo
	}
	return false
}

// errorOf is the error of an answer that isn't a success: a plan_changed is a
// *core.PlanChangedError with the new plan, refused a *core.RefusedError.
func errorOf(status int, body []byte, e *apiv1.Error) error {
	if e == nil || e.Error == "" {
		msg := strings.TrimSpace(string(body))
		if len(msg) > 200 || msg == "" {
			msg = http.StatusText(status)
		}
		return &Error{Status: status, Message: fmt.Sprintf("mangal serve: %d %s", status, msg)}
	}
	switch {
	case e.Code != nil && *e.Code == "plan_changed" && e.Plan != nil:
		return &core.PlanChangedError{Plan: *e.Plan}
	case e.Code != nil && *e.Code == "refused" && e.Reasons != nil:
		return &core.RefusedError{Reasons: *e.Reasons}
	}
	code := ""
	if e.Code != nil {
		code = *e.Code
	}
	return &Error{Status: status, Code: code, Message: e.Error}
}

// answer is the success value of a response, else its error.
func answer[T any](status int, body []byte, ok *T, e *apiv1.Error) (T, error) {
	if status < 300 && ok != nil {
		return *ok, nil
	}
	var zero T
	if status < 300 {
		return zero, &Error{Status: status, Message: fmt.Sprintf("mangal serve: an answer this mangal doesn't read (%d)", status)}
	}
	return zero, errorOf(status, body, e)
}

// Options say which server and how.
type Options struct {
	// URL is the server's address (where its web UI is), e.g. https://mangal.example.com.
	URL string
	// Token is sent as a bearer token, empty for none.
	Token string
	// CAFile is a PEM file of certificates to trust besides the system's.
	CAFile string
	// Timeout is per request, 0 for none.
	Timeout time.Duration
	// Warn, if set, is told the warnings (a token sent over plain HTTP), once each.
	Warn func(msg string)
}

// Client is a server's API.
type Client struct {
	// URL is the server's address, normalized (NormalizeURL).
	URL string
	// HTTP sends the requests: the CA file, the timeout, redirects only on the same
	// address. It doesn't add the token.
	HTTP *http.Client
	// Config is the server's settings for config, once ReadConfig ran.
	Config *ConfigStore
	API    *apiv1.ClientWithResponses

	token    string
	warn     func(string)
	warnOnce sync.Once

	jobsOnce sync.Once
	jobs     *Jobs

	mu sync.Mutex
	// last is the last status the server answered
	last *apiv1.Status
}

// NormalizeURL is the address as the saved tokens are keyed: lowercase scheme and host,
// no default port, no trailing slash, query or fragment. Credentials in it are refused.
func NormalizeURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("%q isn't an http or https address: %w", raw, core.ErrInvalid)
	}
	if u.User != nil {
		return "", fmt.Errorf("the address has a user or password: log in with a token instead (mangal connection login): %w", core.ErrInvalid)
	}
	u.Scheme, u.Host = strings.ToLower(u.Scheme), strings.ToLower(u.Host)
	if port := u.Port(); (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		u.Host = u.Hostname()
		if strings.Contains(u.Host, ":") {
			u.Host = "[" + u.Host + "]"
		}
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath, u.RawQuery, u.Fragment, u.RawFragment, u.ForceQuery = "", "", "", "", false
	return u.String(), nil
}

// New is the client of the server, nothing is sent yet.
func New(o Options) (*Client, error) {
	base, err := NormalizeURL(o.URL)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if o.CAFile != "" {
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		pem, err := os.ReadFile(o.CAFile)
		if err != nil {
			return nil, fmt.Errorf("remote.ca_file: %w", err)
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("remote.ca_file: no PEM certificate in %s", o.CAFile)
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	c := &Client{
		URL:   base,
		HTTP:  &http.Client{Transport: transport, Timeout: o.Timeout, CheckRedirect: sameAddress},
		token: o.Token,
		warn:  o.Warn,
	}
	c.API, err = apiv1.NewClientWithResponses(base+"/api", apiv1.WithHTTPClient(c.HTTP), apiv1.WithRequestEditorFn(c.edit))
	if err != nil {
		return nil, err
	}
	return c, nil
}

// HasToken is true when the client sends a token.
func (c *Client) HasToken() bool { return c.token != "" }

// edit adds the user agent and the token, warning once when the token goes over plain
// HTTP to another machine.
func (c *Client) edit(_ context.Context, req *http.Request) error {
	req.Header.Set("User-Agent", meta.AppName+"/"+meta.Version)
	if c.token == "" {
		return nil
	}
	if req.URL.Scheme == "http" && !loopback(req.URL.Hostname()) {
		c.warnOnce.Do(func() {
			msg := fmt.Sprintf("the token goes to %s over plain HTTP: anyone on the network path can read it (use HTTPS)", req.URL.Host)
			log.Warn("remote: %s", msg)
			if c.warn != nil {
				c.warn(msg)
			}
		})
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	return nil
}

func loopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// sameAddress follows a redirect only to the same scheme, host and port: the token (copied
// to the redirected request) never goes anywhere else.
func sameAddress(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if first := via[0].URL; req.URL.Scheme != first.Scheme || req.URL.Host != first.Host {
		return fmt.Errorf("the server redirects to %s://%s: not followed, use the address that answers", req.URL.Scheme, req.URL.Host)
	}
	return nil
}

// Status is the server's status, refused when it speaks another major API version.
func (c *Client) Status(ctx context.Context) (apiv1.Status, error) {
	r, err := c.API.GetStatusWithResponse(ctx)
	if err != nil {
		return apiv1.Status{}, fmt.Errorf("mangal serve: %w", err)
	}
	st, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	if err != nil {
		return st, err
	}
	if major, _, _ := strings.Cut(st.ApiVersion, "."); major != APIMajor {
		return st, fmt.Errorf("the server speaks API %s, this mangal %s: update the older one", st.ApiVersion, APIMajor)
	}
	c.mu.Lock()
	c.last = &st
	c.mu.Unlock()
	return st, nil
}

// status is the status the server last answered, asked for when there's none.
func (c *Client) status(ctx context.Context) (apiv1.Status, error) {
	c.mu.Lock()
	last := c.last
	c.mu.Unlock()
	if last != nil {
		return *last, nil
	}
	return c.Status(ctx)
}

// Watches are the server's watches (watch.Remote) over the client's HTTP.
func (c *Client) Watches() *watch.Remote {
	w := watch.NewRemote(c.URL, c.token)
	w.Client = c.HTTP
	return w
}

// Me is who the server takes the token for.
func (c *Client) Me(ctx context.Context) (apiv1.Me, error) {
	r, err := c.API.GetMeWithResponse(ctx)
	if err != nil {
		return apiv1.Me{}, fmt.Errorf("mangal serve: %w", err)
	}
	return answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
}

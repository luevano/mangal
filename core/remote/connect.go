package remote

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/util/cache"
)

// Target says which mangal serve: URL (--remote), else remote.url unless Local (--local).
type Target struct {
	URL   string
	Local bool
	// Warn, if set, is told the client's warnings (a token over plain HTTP).
	Warn func(msg string)
}

// Address is the target's server, normalized, "" for this machine.
func (t Target) Address() (string, error) {
	raw := t.URL
	if raw == "" && !t.Local {
		raw = config.Remote.URL.Get()
	}
	if raw == "" {
		return "", nil
	}
	return NormalizeURL(raw)
}

// Token is the token for the server at url (normalized) and where it's from: remote.token
// when it's remote.url's (never sent to another server), else the one `mangal remote
// login` saved; "" for none.
func Token(url string) (token, from string, err error) {
	if t := config.Remote.Token.Get(); t != "" {
		if configured, err := NormalizeURL(config.Remote.URL.Get()); err == nil && configured == url {
			return t, "remote.token", nil
		}
	}
	if token, err = cache.RemoteToken(url); err != nil || token == "" {
		return "", "", err
	}
	return token, "saved", nil
}

// Open is the client of the server at url with the token and the remote.* settings,
// nothing is sent yet.
func Open(url, token string, warn func(string)) (*Client, error) {
	timeout, err := time.ParseDuration(config.Remote.Timeout.Get())
	if err != nil {
		return nil, fmt.Errorf("remote.timeout: %w", err)
	}
	return New(Options{URL: url, Token: token, CAFile: config.Remote.CAFile.Get(), Timeout: timeout, Warn: warn})
}

// Connect is the client of the target's server with its token, checked (the API's major
// version, the token), nil for this machine.
func Connect(ctx context.Context, t Target) (*Client, error) {
	url, err := t.Address()
	if err != nil || url == "" {
		return nil, err
	}
	token, _, err := Token(url)
	if err != nil {
		return nil, err
	}
	c, err := Open(url, token, t.Warn)
	if err != nil {
		return nil, err
	}
	if _, err := c.Status(ctx); err != nil {
		if errors.Is(err, ErrUnauthorized) {
			return nil, fmt.Errorf("%s: %w (mangal remote login %s)", url, err, url)
		}
		return nil, fmt.Errorf("%s: %w", url, err)
	}
	return c, nil
}

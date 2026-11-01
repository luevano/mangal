package remote

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/util/cache"
)

// Target says which backend: Remote (--remote, a name or an address), else the connection
// key unless Local (--local).
type Target struct {
	Remote string
	Local  bool
	// Warn, if set, is told the client's warnings (a token over plain HTTP).
	Warn func(msg string)
}

// Address is the target's server, normalized, "" for this machine.
func (t Target) Address() (string, error) {
	if t.Local {
		return "", nil
	}
	if t.Remote != "" {
		return Resolve(t.Remote)
	}
	return Resolve(config.Connection.Get())
}

// Resolve is the server of a connection value (local, a name from connections, an address),
// normalized, "" for this machine.
func Resolve(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == config.LocalConnection {
		return "", nil
	}
	if strings.ContainsAny(value, ":/") {
		return NormalizeURL(value)
	}
	address, ok := config.Connections.Get()[strings.ToLower(value)]
	if !ok {
		return "", fmt.Errorf("no connection %q: mangal connection ls shows them, mangal connection add adds one", value)
	}
	return NormalizeURL(address)
}

// Name is how to call the server at url: its connection's name, else its host.
func Name(url string) string {
	conns := config.Connections.Get()
	for _, name := range slices.Sorted(maps.Keys(conns)) {
		if address, err := NormalizeURL(conns[name]); err == nil && address == url {
			return name
		}
	}
	_, host, _ := strings.Cut(url, "://")
	return host
}

// Token is the token for the server at url (normalized) and where it's from: remote.token
// when it's the connection key's server (never sent to a --remote elsewhere), else the one
// `mangal connection login` saved; "" for none.
func Token(url string) (token, from string, err error) {
	if t := config.Remote.Token.Get(); t != "" {
		if configured, err := Resolve(config.Connection.Get()); err == nil && configured == url {
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
// version, the token), nil for this machine. config answers the server's keys from then on.
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
			return nil, fmt.Errorf("%s: %w (mangal connection login %s)", Name(url), err, Name(url))
		}
		return nil, fmt.Errorf("%s (--local for this machine): %w", Name(url), err)
	}
	if err := c.ReadConfig(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

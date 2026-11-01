package remote

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	neturl "net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/luevano/mangal/v5/web/apiv1"
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

// connectTimeout is how long a server may take to say it's there: an unreachable one tells
// the user soon, whatever remote.timeout allows a request.
const connectTimeout = 5 * time.Second

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
	handshake, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if _, err := c.Status(handshake); err != nil {
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

// LocalServe is the client of this data's running mangal serve (its discovery file's address
// and key), nil when none runs; an error when it can't be used (another major API version).
func LocalServe(ctx context.Context) (*Client, error) {
	found, d, err := watch.Find(ctx, watch.DataDB())
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, nil
	}
	c, err := New(Options{URL: d.Address, Token: d.Key, Timeout: found.Client.Timeout})
	if err != nil {
		return nil, err
	}
	// its own certificate, when it has one
	c.HTTP.Transport = cmp.Or[http.RoundTripper](found.Client.Transport, c.HTTP.Transport)
	if _, err := c.Status(ctx); err != nil {
		return nil, fmt.Errorf("mangal serve at %s: %w", d.Address, err)
	}
	return c, nil
}

// writeConnection writes a connection key, creating the config file if needed.
func writeConnection(key string, value any) error {
	if err := config.CreateFile(); err != nil {
		return err
	}
	return config.WriteKey(key, value)
}

// AddConnection names the server at address, never over another address.
func AddConnection(name, address string) error {
	if err := config.ValidConnectionName(name); err != nil {
		return err
	}
	url, err := NormalizeURL(address)
	if err != nil {
		return err
	}
	conns := maps.Clone(config.Connections.Get())
	if old, ok := conns[name]; ok && old != url {
		return fmt.Errorf("%s is %s already: remove it first", name, old)
	}
	conns[name] = url
	return writeConnection(config.Connections.Key, conns)
}

// RemoveConnection forgets the name, refused while it's the connection. Its saved token stays
// (it's by address: another name, an ad-hoc --remote) until a logout.
func RemoveConnection(name string) error {
	conns := maps.Clone(config.Connections.Get())
	if _, ok := conns[name]; !ok {
		return fmt.Errorf("no connection %q", name)
	}
	if config.Connection.Get() == name {
		return fmt.Errorf("%s is in use: switch to another first", name)
	}
	delete(conns, name)
	return writeConnection(config.Connections.Key, conns)
}

// UseConnection makes value (local, a name, an address) the connection from now on.
func UseConnection(value string) error {
	value = strings.ToLower(strings.TrimSpace(value))
	url, err := Resolve(value)
	if err != nil {
		return err
	}
	if strings.ContainsAny(value, ":/") {
		value = url
	}
	return writeConnection(config.Connection.Key, value)
}

// SaveToken checks the API token on the server at url and saves it for that address, who is
// the token's name there. Nothing is saved when the server doesn't take it.
func SaveToken(ctx context.Context, url, token string, warn func(string)) (who string, err error) {
	c, err := Open(url, token, warn)
	if err != nil {
		return "", err
	}
	me, err := c.Me(ctx)
	if err != nil {
		return "", fmt.Errorf("%s: %w", url, err)
	}
	if me.Kind != "token" {
		return "", fmt.Errorf("%s doesn't take the token (it's %s there): nothing saved", url, me.Kind)
	}
	if err := cache.SetRemoteToken(url, token); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s (%s)", me.Name, strings.Join(me.Scopes, ", ")), nil
}

// CheckPasswordAddress refuses sending a password to the server at url in the clear: over plain
// HTTP only to this machine. Asked before the password is, Login checks it too.
func CheckPasswordAddress(url string) error {
	u, err := neturl.Parse(url)
	if err != nil {
		return err
	}
	if u.Scheme == "http" && !loopback(u.Hostname()) {
		return fmt.Errorf("a password isn't sent to %s over plain HTTP, anyone on the way could read it: use HTTPS, or an API token (mangal token add there)", u.Host)
	}
	return nil
}

// Login logs the user in on the server at url: the password goes once and isn't kept, the device
// token the server answers (tui@<hostname>, with the user's permissions) is saved as SaveToken
// does. who is the token's name there.
func Login(ctx context.Context, url, user, password string, warn func(string)) (who string, err error) {
	if err := CheckPasswordAddress(url); err != nil {
		return "", err
	}
	host, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("this machine's name: %w", err)
	}
	c, err := Open(url, "", warn)
	if err != nil {
		return "", err
	}
	r, err := c.API.LoginDeviceWithResponse(ctx, apiv1.DeviceLogin{Name: user, Password: password, Device: host})
	if err != nil {
		return "", fmt.Errorf("%s: %w", url, err)
	}
	added, err := answer(r.StatusCode(), r.Body, r.JSON201, r.JSONDefault)
	if err != nil {
		return "", fmt.Errorf("%s: %w", url, err)
	}
	return SaveToken(ctx, url, added.Secret, warn)
}

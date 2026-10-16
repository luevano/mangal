// Package netguard keeps the providers' (and the web image proxy's) requests off private
// addresses: a site's urls (scraped links and images, cdn lists, redirects) can't make
// mangal reach the LAN (the router, FlareSolverr) or the host itself.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"

	"code.dny.dev/ssrf"
	"github.com/luevano/libmangal/netutil"
	"golang.org/x/net/http/httpproxy"
)

// ErrRefused is matched by the errors of a refused connection.
var ErrRefused = errors.New("not a public address (see providers.allow_private_addresses)")

// Options are what a Guard lets through.
type Options struct {
	// AllowPrivate turns the guard off.
	AllowPrivate bool
	// Allow are host names, IPs or CIDRs reached even when private: a host name is dialed
	// as is (whatever it resolves to), an IP or CIDR is checked on the resolved address.
	Allow []string
}

// Guard refuses the IANA special-purpose ranges (v4 and v6: loopback, private, link-local,
// ...), IPv6 outside 2000::/3 (IPv4-mapped/compatible, NAT64 included) and 6to4, on any
// port, checked on the resolved address of every connection, redirects included.
type Guard struct {
	off      bool
	guardian *ssrf.Guardian
	hosts    map[string]bool
}

// New is the guard of options, an error for an Allow entry that isn't a host, IP or CIDR.
func New(options Options) (*Guard, error) {
	g := &Guard{off: options.AllowPrivate, hosts: map[string]bool{}}
	var v4, v6 []netip.Prefix
	for _, entry := range options.Allow {
		prefix, err := ParseAllow(entry)
		if err != nil {
			return nil, err
		}
		switch {
		case !prefix.IsValid():
			g.hosts[strings.ToLower(entry)] = true
		case prefix.Addr().Is4():
			v4 = append(v4, prefix)
		default:
			v6 = append(v6, prefix)
		}
	}
	g.guardian = ssrf.New(ssrf.WithAnyPort(), ssrf.WithAllowedV4Prefixes(v4...), ssrf.WithAllowedV6Prefixes(v6...))
	return g, nil
}

// ParseAllow is the prefix of an IP or CIDR entry, the zero prefix for a host name.
func ParseAllow(entry string) (netip.Prefix, error) {
	if prefix, err := netip.ParsePrefix(entry); err == nil {
		return prefix.Masked(), nil
	}
	if addr, err := netip.ParseAddr(entry); err == nil {
		addr = addr.Unmap()
		return netip.PrefixFrom(addr, addr.BitLen()), nil
	}
	if entry == "" || strings.ContainsAny(entry, "/:@ ") {
		return netip.Prefix{}, fmt.Errorf("%q isn't a host name, IP or CIDR", entry)
	}
	return netip.Prefix{}, nil
}

// Off is true when the guard lets everything through.
func (g *Guard) Off() bool {
	return g.off
}

// Allowed reports whether a connection to addr is allowed.
func (g *Guard) Allowed(addr netip.Addr) bool {
	return g.off || g.guardian.SafeAddr(addr) == nil
}

// Control is a net.Dialer Control refusing the addresses the guard doesn't allow.
func (g *Guard) Control(network, address string, c syscall.RawConn) error {
	if g.off {
		return nil
	}
	if err := g.guardian.Safe(network, address, c); err != nil {
		return fmt.Errorf("%w: %w", ErrRefused, err)
	}
	return nil
}

// Client is a copy of client whose connections only reach what the guard allows. The
// hosts of Options.Allow and of the environment's proxies are dialed as they are (the user
// chose them). A client with a transport other than *http.Transport (or a
// netutil.LoggingTransport over one) can't be guarded and is returned as is.
func (g *Guard) Client(client *http.Client) *http.Client {
	if g.off {
		return client
	}
	transport, ok := g.transport(client.Transport)
	if !ok {
		return client
	}
	guarded := *client
	guarded.Transport = transport
	return &guarded
}

// transport is a guarded copy of rt, false if it can't be guarded. A
// netutil.LoggingTransport (--debug) is kept outside, its Next guarded.
func (g *Guard) transport(rt http.RoundTripper) (http.RoundTripper, bool) {
	var transport *http.Transport
	switch t := rt.(type) {
	case nil:
		transport = http.DefaultTransport.(*http.Transport).Clone()
	case *http.Transport:
		transport = t.Clone()
	case *netutil.LoggingTransport:
		next, ok := g.transport(t.Next)
		if !ok {
			return nil, false
		}
		logging := *t
		logging.Next = next
		return &logging, true
	default:
		return nil, false
	}

	trusted := g.hosts
	if transport.Proxy != nil {
		trusted = make(map[string]bool, len(g.hosts)+2)
		for host := range g.hosts {
			trusted[host] = true
		}
		proxies := httpproxy.FromEnvironment()
		for _, proxy := range []string{proxies.HTTPProxy, proxies.HTTPSProxy} {
			if u, err := url.Parse(proxy); err == nil && u.Hostname() != "" {
				trusted[strings.ToLower(u.Hostname())] = true
			}
		}
	}

	open := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	guarded := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second, Control: g.Control}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if host, _, err := net.SplitHostPort(address); err == nil && trusted[strings.ToLower(host)] {
			return open.DialContext(ctx, network, address)
		}
		return guarded.DialContext(ctx, network, address)
	}
	return transport, true
}

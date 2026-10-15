package web

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"

	"code.dny.dev/ssrf"
	"github.com/luevano/mangal/v5/provider/loader"
)

// guardian: the image proxy only fetches from public addresses, so a page can't use it to
// reach the LAN (the router, FlareSolverr) or the host itself. It refuses the IANA special-purpose ranges (v4 and v6), IPv6 outside 2000::/3
// (IPv4-mapped/compatible, NAT64 included) and 6to4, on any port.
var guardian = ssrf.New(ssrf.WithAnyPort())

func isPublic(addr netip.Addr) bool {
	return guardian.SafeAddr(addr) == nil
}

// publicOnly is a dialer Control that refuses non-public addresses: checked on the
// resolved address of every connection, redirects included.
func publicOnly(network, address string, c syscall.RawConn) error {
	if err := guardian.Safe(network, address, c); err != nil {
		return fmt.Errorf("%w: %w", err, errBadRequest)
	}
	return nil
}

// imageClient is the image proxy's client: public addresses only, no proxy from the
// environment (it would reach the private addresses for it).
func imageClient() *http.Client {
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second, Control: publicOnly}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = dialer.DialContext
	c := loader.HTTPClient()
	c.Transport = loader.DebugTransport(transport)
	return c
}

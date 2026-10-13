package web

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"

	"github.com/luevano/mangal/v5/provider/loader"
)

// errPrivateAddress: the image proxy only fetches from public addresses, so a page can't
// use it to reach the LAN (the router, FlareSolverr) or the host itself.
var errPrivateAddress = errors.New("not a public address")

// sharedAddressSpace is 100.64.0.0/10 (carrier-grade NAT, Tailscale).
var sharedAddressSpace = netip.MustParsePrefix("100.64.0.0/10")

func isPublic(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsGlobalUnicast() && !addr.IsPrivate() && !sharedAddressSpace.Contains(addr)
}

// publicOnly is a dialer Control that refuses non-public addresses: checked on the
// resolved address of every connection, redirects included.
func publicOnly(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("%s: %w", address, err)
	}
	if !isPublic(ap.Addr()) {
		return fmt.Errorf("%s: %w: %w", ap.Addr(), errPrivateAddress, errBadRequest)
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
	c.Transport = transport
	return c
}

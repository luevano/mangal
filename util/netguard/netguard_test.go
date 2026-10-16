package netguard

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"code.dny.dev/ssrf"
	"github.com/luevano/libmangal/netutil"
)

func newGuard(t *testing.T, options Options) *Guard {
	t.Helper()
	g, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestAllowed(t *testing.T) {
	g := newGuard(t, Options{})
	for addr, want := range map[string]bool{
		"93.184.215.14":   true,
		"2606:4700::1111": true,
		"127.0.0.1":       false,
		"10.0.0.1":        false,
		"192.168.1.100":   false,
		"172.16.0.1":      false,
		"169.254.169.254": false,
		"100.100.1.1":     false,
		"0.0.0.0":         false,
		"::1":             false,
		"fe80::1":         false,
		"fd00::1":         false,
		"::ffff:10.0.0.1": false,
		"224.0.0.1":       false,
		"8.8.8.8":         true,
		"1.1.1.1":         true,
		"2001:4860::8888": true,
		"0.0.0.1":         false,
		"192.0.0.8":       false,
		"198.18.0.1":      false,
		"240.0.0.1":       false,
		"192.0.2.1":       false,
		"64:ff9b::7f00:1": false, // NAT64 of 127.0.0.1
		"2002:7f00:1::":   false, // 6to4 of 127.0.0.1
		"::127.0.0.1":     false, // IPv4-compatible
		"::ffff:8.8.8.8":  false,
		"2001:db8::1":     false,
		"ff02::1":         false,
	} {
		if got := g.Allowed(netip.MustParseAddr(addr)); got != want {
			t.Errorf("%s: %t", addr, got)
		}
	}

	g = newGuard(t, Options{Allow: []string{"192.168.1.0/24", "::1", "10.0.0.1"}})
	for addr, want := range map[string]bool{
		"192.168.1.100": true,
		"192.168.2.1":   false,
		"::1":           true,
		"10.0.0.1":      true,
		"10.0.0.2":      false,
		"127.0.0.1":     false,
	} {
		if got := g.Allowed(netip.MustParseAddr(addr)); got != want {
			t.Errorf("allowlist %s: %t", addr, got)
		}
	}
	if !newGuard(t, Options{AllowPrivate: true}).Allowed(netip.MustParseAddr("127.0.0.1")) {
		t.Error("AllowPrivate refused loopback")
	}
}

func TestParseAllow(t *testing.T) {
	for _, entry := range []string{"", "http://x", "a b", "x/y", "user@host"} {
		if _, err := ParseAllow(entry); err == nil {
			t.Errorf("%q: no error", entry)
		}
	}
	for entry, want := range map[string]string{
		"komga.lan":      "invalid Prefix",
		"10.1.2.3/8":     "10.0.0.0/8",
		"192.168.1.5":    "192.168.1.5/32",
		"::ffff:1.2.3.4": "1.2.3.4/32",
		"fd00::/8":       "fd00::/8",
	} {
		got, err := ParseAllow(entry)
		if err != nil || got.String() != want {
			t.Errorf("%q: %s, %v", entry, got, err)
		}
	}
	if _, err := New(Options{Allow: []string{"http://x"}}); err == nil {
		t.Error("New took a bad entry")
	}
}

func TestClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://127.0.0.1:"+r.Host[strings.LastIndex(r.Host, ":")+1:]+"/", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("png"))
	}))
	defer server.Close()
	port := server.URL[strings.LastIndex(server.URL, ":")+1:]
	get := func(client *http.Client, url string) error {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
		}
		return err
	}
	refused := func(err error) bool {
		return errors.Is(err, ErrRefused) && errors.Is(err, ssrf.ErrProhibitedIP)
	}

	base := &http.Client{}
	if err := get(newGuard(t, Options{}).Client(base), server.URL); !refused(err) {
		t.Errorf("loopback: got %v, want refused", err)
	}
	if base.Transport != nil {
		t.Error("the given client was changed")
	}
	if err := get(newGuard(t, Options{AllowPrivate: true}).Client(base), server.URL); err != nil {
		t.Errorf("allowed: %v", err)
	}
	if err := get(newGuard(t, Options{Allow: []string{"127.0.0.0/8"}}).Client(base), server.URL); err != nil {
		t.Errorf("allowed cidr: %v", err)
	}

	// an allowed host (e.g. a mirror) is dialed as is, its redirects elsewhere are guarded
	mirror := newGuard(t, Options{Allow: []string{"LOCALHOST"}}).Client(base)
	if err := get(mirror, "http://localhost:"+port+"/"); err != nil {
		t.Errorf("allowed host: %v", err)
	}
	if err := get(mirror, "http://localhost:"+port+"/redirect"); !refused(err) {
		t.Errorf("redirect from an allowed host: got %v, want refused", err)
	}

	// a logging transport is guarded underneath, and still logs
	logged := 0
	logging := &http.Client{Transport: netutil.NewLoggingTransport(nil, func(string, ...any) { logged++ }, nil)}
	if err := get(newGuard(t, Options{}).Client(logging), server.URL); !refused(err) {
		t.Errorf("logging transport: got %v, want refused", err)
	}
	if logged != 1 {
		t.Errorf("logged %d requests, want 1", logged)
	}
	if logging.Transport.(*netutil.LoggingTransport).Next != nil {
		t.Error("the given logging transport was changed")
	}

	// a transport it can't guard stays
	custom := &http.Client{Transport: roundTripperFunc(nil)}
	if newGuard(t, Options{}).Client(custom) != custom {
		t.Error("a custom transport should be kept")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

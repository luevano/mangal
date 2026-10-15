package web

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/adrg/xdg"
	"github.com/labstack/echo/v5"
	"github.com/luevano/mangal/v5/client"
)

func TestMain(m *testing.M) {
	// the clients open mangal's databases: keep them away from the user's
	dir, err := os.MkdirTemp("", "mangal-web-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("MANGAL_CACHE_PATH", filepath.Join(dir, "cache"))
	os.Setenv("MANGAL_AUTH_KEYRING", "false")
	xdg.DataHome = filepath.Join(dir, "data")
	xdg.StateHome = filepath.Join(dir, "state")

	code := m.Run()
	_ = client.CloseAll()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// newTestServer serves the image proxy as the API does (its errors, its headers).
func newTestServer(t *testing.T, s *Server) *httptest.Server {
	t.Helper()
	e := echo.New()
	e.HTTPErrorHandler = APIError
	e.Use(ImageHeaders)
	e.GET("/api/image", func(c *echo.Context) error {
		data, contentType, err := s.Image(c.Request().Context(), c.QueryParam("url"), c.QueryParam("referer"))
		if err != nil {
			return err
		}
		return c.Blob(http.StatusOK, contentType, data)
	})
	server := httptest.NewServer(e)
	t.Cleanup(server.Close)
	return server
}

func pngImage(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.Black)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestImage(t *testing.T) {
	data := pngImage(t)
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/a.webp":
			if r.Referer() != "https://site.example/manga" {
				http.Error(w, "no referer", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "image/webp")
			w.Write(data)
		case "/sniffed":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(data)
		case "/x.svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
		case "/svg-octet":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write([]byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
		case "/html":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html>cloudflare</html>"))
		case "/big":
			w.Header().Set("Content-Type", "image/png")
			w.Write(make([]byte, maxImageSize+1))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	server := newTestServer(t, &Server{images: newImageCache(1 << 20), httpClient: http.DefaultClient})

	getImage := func(path, referer string) (*http.Response, []byte) {
		t.Helper()
		q := url.Values{"url": {upstream.URL + path}}
		if referer != "" {
			q.Set("referer", referer)
		}
		resp, err := http.Get(server.URL + "/api/image?" + q.Encode())
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body bytes.Buffer
		body.ReadFrom(resp.Body)
		return resp, body.Bytes()
	}

	for range 2 {
		resp, body := getImage("/a.webp", "https://site.example/manga")
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/webp" || !bytes.Equal(body, data) {
			t.Fatalf("status %d, type %q, %d bytes", resp.StatusCode, resp.Header.Get("Content-Type"), len(body))
		}
		if resp.Header.Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(resp.Header.Get("Content-Security-Policy"), "sandbox") {
			t.Errorf("headers %v", resp.Header)
		}
	}
	if hits.Load() != 1 {
		t.Errorf("not cached: %d upstream requests", hits.Load())
	}

	if resp, _ := getImage("/sniffed", ""); resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" {
		t.Errorf("sniffed: status %d, type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	for _, path := range []string{"/html", "/missing", "/big", "/x.svg", "/svg-octet"} {
		before := hits.Load()
		for range 2 {
			if resp, _ := getImage(path, ""); resp.StatusCode != http.StatusBadGateway {
				t.Errorf("%s: status %d", path, resp.StatusCode)
			}
		}
		if hits.Load()-before != 2 {
			t.Errorf("%s: error cached", path)
		}
	}

	for _, bad := range []string{"file:///etc/passwd", "gopher://x", "/relative", "http://"} {
		resp, err := http.Get(server.URL + "/api/image?" + url.Values{"url": {bad}}.Encode())
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d", bad, resp.StatusCode)
		}
	}
}

func TestImageCache(t *testing.T) {
	c := newImageCache(10)
	c.add("a", cachedImage{data: make([]byte, 4)})
	c.add("b", cachedImage{data: make([]byte, 4)})
	c.get("a")
	c.add("c", cachedImage{data: make([]byte, 4)})
	if _, ok := c.get("b"); ok {
		t.Error("b not evicted")
	}
	if _, ok := c.get("a"); !ok {
		t.Error("a evicted")
	}
	c.add("huge", cachedImage{data: make([]byte, 11)})
	if _, ok := c.get("huge"); ok || c.used != 8 {
		t.Errorf("huge cached, used %d", c.used)
	}
}

func TestCheckHost(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	loopback := checkHost(GuardOptions{Addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}}, ok)
	for host, want := range map[string]int{
		"127.0.0.1:6969": http.StatusOK,
		"localhost:6969": http.StatusOK,
		"[::1]:6969":     http.StatusOK,
		"evil.example":   http.StatusMisdirectedRequest,
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Host = host
		w := httptest.NewRecorder()
		loopback.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("%s: %d", host, w.Code)
		}
	}
	// a reverse proxy's name, on loopback or any address
	for _, ip := range []net.IP{net.IPv4(127, 0, 0, 1), net.IPv4zero} {
		h := checkHost(GuardOptions{Addr: &net.TCPAddr{IP: ip, Port: 1}, AllowedHosts: []string{"mangal.example.com"}}, ok)
		for host, want := range map[string]int{
			"mangal.example.com":     http.StatusOK,
			"MANGAL.example.com:443": http.StatusOK,
			"localhost:1":            http.StatusOK,
			"evil.example":           http.StatusMisdirectedRequest,
		} {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Host = host
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != want {
				t.Errorf("%s on %s: %d", host, ip, w.Code)
			}
		}
	}
	// no list off loopback: any host
	any := checkHost(GuardOptions{Addr: &net.TCPAddr{IP: net.IPv4zero, Port: 1}}, ok)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "whatever.lan"
	w := httptest.NewRecorder()
	any.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("any host: %d", w.Code)
	}
}

// TestGuardProxy: a trusted proxy's X-Forwarded-Host is the host (checked, and the origin
// compared with it); an allowed origin passes, anyone else's forwarded host is ignored.
func TestGuardProxy(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	_, proxy, _ := net.ParseCIDR("10.0.0.2/32")
	h := Guard(GuardOptions{Addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 6977},
		AllowedHosts: []string{"mangal.example.com"}, AllowedOrigins: []string{"https://public.example.com"},
		TrustedProxies: []*net.IPNet{proxy}})(ok)
	for _, tc := range []struct {
		remote, host, forwarded, origin string
		want                            int
	}{
		{"10.0.0.2:1", "127.0.0.1:6977", "mangal.example.com", "https://mangal.example.com", http.StatusOK},
		{"10.0.0.2:1", "127.0.0.1:6977", "evil.example", "", http.StatusMisdirectedRequest},
		{"10.0.0.9:1", "127.0.0.1:6977", "mangal.example.com", "https://mangal.example.com", http.StatusForbidden},
		{"10.0.0.2:1", "127.0.0.1:6977", "mangal.example.com", "https://public.example.com", http.StatusOK},
		{"10.0.0.2:1", "127.0.0.1:6977", "mangal.example.com", "https://evil.example", http.StatusForbidden},
	} {
		r := httptest.NewRequest(http.MethodPost, "/api/x", nil)
		r.RemoteAddr, r.Host = tc.remote, tc.host
		r.Header.Set("X-Forwarded-Host", tc.forwarded)
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%+v: %d", tc, w.Code)
		}
	}
}

// TestCrossOrigin: unsafe requests from another site's page are refused, by Sec-Fetch-Site
// or, from browsers without it, by Origin; safe methods and non-browser clients pass.
func TestCrossOrigin(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	h := Guard(GuardOptions{Addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 6970}})(ok)
	for _, tc := range []struct {
		method, site, origin string
		want                 int
	}{
		{"POST", "", "", http.StatusOK},
		{"POST", "same-origin", "http://127.0.0.1:6970", http.StatusOK},
		{"POST", "none", "", http.StatusOK},
		{"POST", "cross-site", "https://evil.example", http.StatusForbidden},
		{"POST", "same-site", "http://127.0.0.1:6971", http.StatusForbidden},
		{"DELETE", "cross-site", "https://evil.example", http.StatusForbidden},
		{"POST", "", "http://127.0.0.1:6970", http.StatusOK},
		{"POST", "", "http://127.0.0.1:6971", http.StatusForbidden},
		{"POST", "", "https://evil.example", http.StatusForbidden},
		{"POST", "", "null", http.StatusForbidden},
		{"GET", "cross-site", "https://evil.example", http.StatusOK},
	} {
		r := httptest.NewRequest(tc.method, "/api/x", nil)
		r.Host = "127.0.0.1:6970"
		if tc.site != "" {
			r.Header.Set("Sec-Fetch-Site", tc.site)
		}
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%s site %q origin %q: %d, want %d", tc.method, tc.site, tc.origin, w.Code, tc.want)
		}
	}
}

func TestImagePublicOnly(t *testing.T) {
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
		if got := isPublic(netip.MustParseAddr(addr)); got != want {
			t.Errorf("%s: %t", addr, got)
		}
	}

	// the upstream is on loopback, directly or through a redirect
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngImage(t))
	}))
	defer upstream.Close()
	server := newTestServer(t, &Server{images: newImageCache(1 << 20), httpClient: imageClient()})
	for _, target := range []string{upstream.URL + "/x.png", "http://localhost:" + upstream.URL[strings.LastIndex(upstream.URL, ":")+1:] + "/x.png"} {
		resp, err := http.Get(server.URL + "/api/image?" + url.Values{"url": {target}}.Encode())
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d", target, resp.StatusCode)
		}
	}
}

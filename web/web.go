package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/skratchdot/open-golang/open"
)

type Args struct {
	Open bool
	Host string
	Port string
}

// Run serves until the context is cancelled (ctrl+c), which also cancels the requests in
// flight. Messages for the user go to out.
func Run(ctx context.Context, args Args, out io.Writer) error {
	listener, err := net.Listen("tcp", net.JoinHostPort(args.Host, args.Port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	handler, err := Handler(out, GuardOptions{Addr: listener.Addr()})
	if err != nil {
		listener.Close()
		return err
	}

	server := NewServer(ctx, handler)
	address := "http://" + listener.Addr().String()
	fmt.Fprintf(out, "Serving on %s\n", address)
	if args.Open {
		if err := open.Start(address); err != nil {
			log.Warn("open %s: %s", address, err)
		}
	}
	return Serve(ctx, server, listener, nil)
}

// NewServer is the server of the handler (guarded by it, see Guard); its requests get ctx.
func NewServer(ctx context.Context, handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
}

// Serve serves on the listener until ctx is cancelled or the server fails. Then stopping
// (if not nil) runs, while the server still answers, and the server is shut down, giving
// the requests in flight a few seconds. The error is the server's, if it failed.
func Serve(ctx context.Context, server *http.Server, listener net.Listener, stopping func()) error {
	errs := make(chan error, 1)
	go func() { errs <- server.Serve(listener) }()

	var serveErr error
	select {
	case serveErr = <-errs:
	case <-ctx.Done():
	}
	if stopping != nil {
		stopping()
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdownErr := server.Shutdown(shutdown)
	if serveErr == nil {
		serveErr = <-errs
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	if serveErr == nil && shutdownErr != nil {
		return fmt.Errorf("stop the server: %w", shutdownErr)
	}
	return serveErr
}

// Handler is the web UI with its API under /api, as Run serves it, guarded (Guard).
// Providers that can't be loaded are told to out.
func Handler(out io.Writer, guard GuardOptions) (http.Handler, error) {
	server, err := NewBrowser(out)
	if err != nil {
		return nil, err
	}
	e, err := newHandler(server)
	if err != nil {
		return nil, err
	}
	e.Pre(echo.WrapMiddleware(Guard(guard)))
	return e, nil
}

// newHandler is the API under /api and the UI.
func newHandler(server *Server) (*echo.Echo, error) {
	e := echo.New()
	e.HTTPErrorHandler = APIError
	e.Use(ImageHeaders)
	if err := server.Register(e, nil); err != nil {
		return nil, err
	}
	return e, nil
}

// NewBrowser is the browse API of the providers and its image proxy (Register adds them).
// Providers that can't be loaded are told to out.
func NewBrowser(out io.Writer) (*Server, error) {
	loaders, problems, err := manager.Load()
	if err != nil {
		return nil, err
	}
	for _, p := range problems {
		fmt.Fprintf(out, "warning: skipped %s\n", p)
	}
	return &Server{
		loaders:    loaders,
		images:     newImageCache(imageCacheSize),
		httpClient: imageClient(),
	}, nil
}

// GuardOptions are what Guard lets in.
type GuardOptions struct {
	// Addr is the server's address.
	Addr net.Addr
	// AllowedHosts are host names answered besides localhost and Addr: on loopback only
	// those are answered (DNS rebinding), set they're checked on any address.
	AllowedHosts []string
	// AllowedOrigins are origins (scheme://host[:port]) whose pages may send unsafe requests.
	AllowedOrigins []string
	// TrustedProxies are the proxies whose X-Forwarded-Host is the host.
	TrustedProxies []*net.IPNet
}

// Guard refuses the requests the server shouldn't answer: for another host (checkHost)
// and unsafe ones (POST, PATCH, DELETE) from another site's pages, which need no CORS
// preflight (http.CrossOriginProtection). Non-browser clients (mangal's remote client,
// curl) send neither Sec-Fetch-Site nor Origin and pass. Behind a trusted proxy the host
// is its X-Forwarded-Host.
func Guard(opts GuardOptions) func(http.Handler) http.Handler {
	cop := http.NewCrossOriginProtection()
	for _, origin := range opts.AllowedOrigins {
		if err := cop.AddTrustedOrigin(origin); err != nil {
			log.Warn("serve.allowed_origins: %s", err)
		}
	}
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reason := "cross-origin request"
		if origin := r.Header.Get("Origin"); origin != "" {
			reason += fmt.Sprintf(" (Origin %q)", origin)
		} else {
			reason += fmt.Sprintf(" (Sec-Fetch-Site %q)", r.Header.Get("Sec-Fetch-Site"))
		}
		LogRefused(r, reason)
		w.WriteHeader(http.StatusForbidden)
	}))
	return func(next http.Handler) http.Handler {
		return forwardedHost(opts.TrustedProxies, checkHost(opts, cop.Handler(next)))
	}
}

// FromProxy is true when the request comes from one of the proxies.
func FromProxy(r *http.Request, proxies []*net.IPNet) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, p := range proxies {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// forwardedHost makes a trusted proxy's X-Forwarded-Host the request's host.
func forwardedHost(proxies []*net.IPNet, next http.Handler) http.Handler {
	if len(proxies) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fh := r.Header.Get("X-Forwarded-Host"); fh != "" && FromProxy(r, proxies) {
			// the first one is the client's
			r.Host = strings.TrimSpace(strings.Split(fh, ",")[0])
		}
		next.ServeHTTP(w, r)
	})
}

// LogRefused logs a refused request at debug level: method, path, client and why, never
// the query or the headers' secrets.
func LogRefused(r *http.Request, reason string) {
	log.Debug("refused %s %s from %s: %s", r.Method, r.URL.Path, r.RemoteAddr, reason)
}

// checkHost refuses requests for another host when serving on a loopback address, so a
// site can't reach the server through DNS rebinding, and on any address for a host not
// in AllowedHosts when it's set.
func checkHost(opts GuardOptions, next http.Handler) http.Handler {
	tcp, ok := opts.Addr.(*net.TCPAddr)
	loopback := ok && tcp.IP.IsLoopback()
	if !loopback && len(opts.AllowedHosts) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsLoopbackHost(r.Host) && !allowedHost(r.Host, opts) {
			LogRefused(r, fmt.Sprintf("unknown host %q", r.Host))
			http.Error(w, "unknown host", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// hostOnly is the host of host[:port], without the brackets of IPv6.
func hostOnly(hostport string) string {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	return strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
}

func allowedHost(hostport string, opts GuardOptions) bool {
	host := hostOnly(hostport)
	if tcp, ok := opts.Addr.(*net.TCPAddr); ok && !tcp.IP.IsUnspecified() && net.ParseIP(host).Equal(tcp.IP) {
		return true
	}
	for _, allowed := range opts.AllowedHosts {
		if strings.EqualFold(host, hostOnly(allowed)) {
			return true
		}
	}
	return false
}

// IsLoopbackHost is true for localhost and loopback addresses (with or without a port).
func IsLoopbackHost(hostport string) bool {
	host := hostOnly(hostport)
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

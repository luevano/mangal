package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

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
	handler, err := Handler(out)
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", net.JoinHostPort(args.Host, args.Port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	server := NewServer(ctx, listener.Addr(), handler)
	address := "http://" + listener.Addr().String()
	fmt.Fprintf(out, "Serving on %s\n", address)
	if args.Open {
		if err := open.Start(address); err != nil {
			log.Warn("open %s: %s", address, err)
		}
	}
	return Serve(ctx, server, listener, nil)
}

// NewServer is the server of the handler on addr, guarded (Guard); its requests get ctx.
func NewServer(ctx context.Context, addr net.Addr, handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           Guard(addr, handler),
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

// Handler is the web UI with its API under /api, as Run serves it (`mangal serve --web`
// mounts it). Providers that can't be loaded are told to out.
func Handler(out io.Writer) (http.Handler, error) {
	loaders, problems, err := manager.Load()
	if err != nil {
		return nil, err
	}
	for _, p := range problems {
		fmt.Fprintf(out, "warning: skipped %s\n", p)
	}
	return newHandler(&Server{
		loaders:    loaders,
		images:     newImageCache(imageCacheSize),
		httpClient: imageClient(),
	})
}

// Guard refuses the requests the server shouldn't answer: for another host when serving
// on a loopback address (checkHost) and from another site's pages (checkOrigin).
func Guard(addr net.Addr, next http.Handler) http.Handler {
	return checkHost(addr, checkOrigin(next))
}

// checkOrigin refuses requests a browser sends from another origin's page: without it any
// site could POST to the server (forms and no-cors fetches need no CORS preflight).
// Requests without an Origin (mangal's own clients, curl, same-origin GETs) pass.
func checkOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(origin, r) {
			http.Error(w, "cross-origin request", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func sameOrigin(origin string, r *http.Request) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return u.Scheme == scheme && u.Host != "" && strings.EqualFold(u.Host, r.Host) && u.Path == ""
}

// checkHost refuses requests for another host when serving on a loopback address, so a
// site can't reach the server through DNS rebinding.
func checkHost(addr net.Addr, next http.Handler) http.Handler {
	tcp, ok := addr.(*net.TCPAddr)
	if !ok || !tcp.IP.IsLoopback() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) {
			http.Error(w, "unknown host", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

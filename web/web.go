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

	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/provider/loader"
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

	server := &http.Server{
		Handler:           checkHost(listener.Addr(), handler),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	errs := make(chan error, 1)
	go func() { errs <- server.Serve(listener) }()

	address := "http://" + listener.Addr().String()
	fmt.Fprintf(out, "Serving on %s\n", address)
	if args.Open {
		if err := open.Start(address); err != nil {
			log.Warn("open %s: %s", address, err)
		}
	}

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		return fmt.Errorf("stop the server: %w", err)
	}
	if err := <-errs; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
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
		httpClient: loader.HTTPClient(),
	})
}

// CheckHost refuses requests for another host when serving on a loopback address (see
// checkHost).
func CheckHost(addr net.Addr, next http.Handler) http.Handler {
	return checkHost(addr, next)
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

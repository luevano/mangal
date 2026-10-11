// Package serve is `mangal serve`: the scheduler that checks the watches when they're due
// and the JSON API to manage them (the TUI and the CLI use it through watch.Connect).
package serve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/meta"
	"github.com/luevano/mangal/v5/notify"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/web"
)

// Args of Run.
type Args struct {
	Host string
	Port int
	// Web also serves the web UI.
	Web bool
	// Token is required by the API (as a bearer token), needed when Host isn't loopback.
	Token string
}

// lockName is the data db lock of the server, one per data db.
const lockName = "serve"

// lockStale is when the lock of a server that stopped beating (another host, or frozen)
// is taken over; a server of this host that doesn't run anymore loses it at once.
const lockStale = 2 * time.Minute

// logger logs to the log file and out.
type logger struct {
	out io.Writer
	mu  *sync.Mutex
}

func newLogger(out io.Writer) logger { return logger{out: out, mu: &sync.Mutex{}} }

func (l logger) print(prefix, line string) {
	if l.out == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintln(l.out, prefix+line)
}

func (l logger) Infof(format string, a ...any) {
	log.Info(format, a...)
	l.print("", fmt.Sprintf(format, a...))
}

func (l logger) Warnf(format string, a ...any) {
	log.Warn(format, a...)
	l.print("warning: ", fmt.Sprintf(format, a...))
}

func (l logger) Errorf(format string, a ...any) {
	log.Error(format, a...)
	l.print("error: ", fmt.Sprintf(format, a...))
}

// Run serves until ctx is cancelled (SIGTERM, ctrl+c): the running checks are cancelled
// (the chapter being downloaded finished or dropped) and recorded, then the server stops.
// It logs to out and the log file.
func Run(ctx context.Context, args Args, out io.Writer) error {
	lg := newLogger(out)
	svc, err := watch.New(notify.Watches())
	if err != nil {
		return err
	}
	svc.OnError = func(err error) { lg.Warnf("%s", err) }

	lock, err := svc.Watches.Lock(lockName, lockStale)
	if err != nil {
		var held *storage.LockError
		if errors.As(err, &held) {
			return fmt.Errorf("another mangal serve (pid %d on %s, since %s) uses %s; a second instance needs another config with its own data.path and serve.port",
				held.Holder.PID, held.Holder.Host, held.Holder.Acquired.Format(time.DateTime), watch.DataDB())
		}
		return err
	}
	defer func() {
		if err := lock.Unlock(); err != nil {
			lg.Warnf("%s", err)
		}
	}()
	stopBeat := heartbeat(lock, lg)
	defer stopBeat()

	listener, err := net.Listen("tcp", net.JoinHostPort(args.Host, fmt.Sprint(args.Port)))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()
	loopback := isLoopback(listener.Addr())
	if !loopback && args.Token == "" {
		return fmt.Errorf("listening on %s (not loopback) needs a token: set serve.token (or MANGAL_SERVE_TOKEN)", listener.Addr())
	}
	token := args.Token
	if loopback {
		token = ""
	}

	var current atomic.Pointer[watch.Service]
	current.Store(svc)
	r := &runs{}
	address := watch.LocalAddress(listener.Addr())
	started := time.Now()
	rl := &reloader{current: &current, log: lg}
	sched := newScheduler(rl.reload, lg, r)
	api := &API{
		Manager: func() watch.Manager { return current.Load() },
		Info:    watch.Status{Server: true, Address: address, Version: meta.Version, Started: started},
		Token:   token,
		Changed: sched.Wake,
		Log:     lg,
		Runs:    r,
		ctx:     ctx,
	}
	mux := http.NewServeMux()
	api.Register(mux)
	if args.Web {
		ui, err := web.Handler(out)
		if err != nil {
			return err
		}
		mux.Handle("/", ui)
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusNotFound, watch.ErrorResponse{Error: "not found (the web UI is off, see serve.web)", Code: watch.CodeNotFound})
		})
	}
	server := &http.Server{
		Handler:           web.CheckHost(listener.Addr(), mux),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	errs := make(chan error, 1)
	go func() { errs <- server.Serve(listener) }()

	dataDB := watch.DataDB()
	discovery := watch.DiscoveryPath(dataDB)
	if err := watch.WriteDiscovery(discovery, watch.Discovery{PID: os.Getpid(), Address: address, Started: started, Version: meta.Version, Data: dataDB}); err != nil {
		lg.Warnf("%s (the TUI and the CLI won't find this server)", err)
	}
	defer removeDiscovery(discovery, lg)

	extra := ""
	if args.Web {
		extra = ", web UI on " + address
	}
	listen := address
	if !loopback {
		listen = fmt.Sprintf("%s (%s, token required)", listener.Addr(), address)
	}
	lg.Infof("mangal serve %s on %s%s, data %s (pid %d)", meta.Version, listen, extra, dataDB, os.Getpid())

	schedDone := make(chan struct{})
	go func() {
		defer close(schedDone)
		sched.Run(ctx)
	}()

	var serveErr error
	select {
	case serveErr = <-errs:
	case <-ctx.Done():
		lg.Infof("stopping")
	}
	<-schedDone
	api.Wait()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		lg.Warnf("stop the server: %s", err)
	}
	if serveErr == nil {
		serveErr = <-errs
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	lg.Infof("stopped")
	return serveErr
}

// heartbeat keeps the lock alive until stopped.
func heartbeat(lock *storage.Lock, lg logger) (stop func()) {
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(lockStale / 4)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := lock.Heartbeat(); err != nil {
					lg.Warnf("%s", err)
				}
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
	}
}

// removeDiscovery removes the discovery file if it's still this process's.
func removeDiscovery(file string, lg logger) {
	d, found, err := watch.ReadDiscovery(file)
	if err != nil || !found || d.PID != os.Getpid() {
		return
	}
	if err := os.Remove(file); err != nil {
		lg.Warnf("%s", err)
	}
}

func isLoopback(addr net.Addr) bool {
	tcp, ok := addr.(*net.TCPAddr)
	return ok && tcp.IP.IsLoopback()
}

// watchKeys are re-read from the config file at each cycle.
var watchKeys = []string{
	config.Watch.Interval.Key, config.Watch.Jitter.Key, config.Watch.GapInterval.Key, config.Watch.Gaps.Key,
	config.Watch.ParallelProviders.Key, config.Watch.Backoff.Key, config.Watch.Notify.Key,
}

// reloader re-reads the watch.* keys from the config file at each cycle (edits from the
// TUI or `config set` apply without a restart; environment variables and flags still win)
// and swaps the service for one with the new options when they changed.
type reloader struct {
	current *atomic.Pointer[watch.Service]
	log     logger
	// last are the values of the last cycle
	last map[string]any
}

func (r *reloader) reload() checker {
	svc := r.current.Load()
	values := map[string]any{}
	for _, key := range watchKeys {
		v, err := config.Get(key)
		if _, env := config.EnvVar(key); !env && err == nil {
			v, err = config.FileValue(key)
		}
		if err == nil {
			v, err = config.Parse(key, v)
		}
		if err != nil {
			r.log.Warnf("re-read the config: %s", err)
			return serviceChecker{svc}
		}
		values[key] = v
	}
	if r.last == nil {
		r.last = map[string]any{}
		for _, key := range watchKeys {
			r.last[key], _ = config.Get(key)
		}
	}
	var changed []string
	for _, key := range watchKeys {
		if fmt.Sprint(r.last[key]) != fmt.Sprint(values[key]) {
			changed = append(changed, fmt.Sprintf("%s = %v", key, values[key]))
		}
	}
	r.last = values
	if len(changed) == 0 {
		return serviceChecker{svc}
	}
	r.log.Infof("config changed: %s", strings.Join(changed, ", "))
	next := *svc
	next.Options = optionsFrom(svc.Options, values)
	r.current.Store(&next)
	return serviceChecker{&next}
}

// optionsFrom are the options with the watch.* values (as config.Parse returns them).
func optionsFrom(opts watch.Options, values map[string]any) watch.Options {
	duration := func(key string, def time.Duration) time.Duration {
		s, _ := values[key].(string)
		d, err := time.ParseDuration(s)
		if err != nil {
			return def
		}
		return d
	}
	integer := func(key string, def int) int {
		switch n := values[key].(type) {
		case int64:
			return int(n)
		case int:
			return n
		}
		return def
	}
	opts.Interval = max(time.Minute, duration(config.Watch.Interval.Key, opts.Interval))
	opts.Jitter = duration(config.Watch.Jitter.Key, opts.Jitter)
	opts.GapInterval = duration(config.Watch.GapInterval.Key, opts.GapInterval)
	if b, ok := values[config.Watch.Gaps.Key].(bool); ok {
		opts.Gaps = b
	}
	opts.ParallelProviders = integer(config.Watch.ParallelProviders.Key, opts.ParallelProviders)
	opts.Backoff = integer(config.Watch.Backoff.Key, opts.Backoff)
	if s, ok := values[config.Watch.Notify.Key].(string); ok {
		opts.Notify = s
	}
	return opts
}

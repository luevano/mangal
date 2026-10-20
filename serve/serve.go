// Package serve is `mangal serve`: the scheduler that checks the watches when they're due
// and the JSON API to manage them (the TUI and the CLI use it through watch.Connect).
package serve

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/meta"
	"github.com/luevano/mangal/v5/notify"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/luevano/mangal/v5/util/cron"
	"github.com/luevano/mangal/v5/util/tlsutil"
	"github.com/luevano/mangal/v5/web"
)

// Args of Run.
type Args struct {
	Host string
	Port int
	// Web also serves the web UI.
	Web bool
	// Auth is AuthLogin or AuthNone.
	Auth string
	// SessionLifetime is how long a remembered login lasts, DefaultSessionLifetime when 0.
	SessionLifetime time.Duration
	// TrustedProxies are the reverse proxies whose X-Forwarded-For and X-Forwarded-Host
	// are believed (the client's address, the host).
	TrustedProxies []string
	// TLSCert and TLSKey are the certificate and key files to serve HTTPS with.
	TLSCert, TLSKey string
}

// lockName is the data db lock of the server, one per data db.
const lockName = watch.ServeLock

// lockStale is when the lock of a server that stopped beating (another host, or frozen)
// is taken over; a server of this host that doesn't run anymore loses it at once.
const lockStale = watch.ServeLockStale

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
	// stopped by the caller, a lost lock or a failed server
	parent := ctx
	ctx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	lg := newLogger(out)
	svc, err := watch.New(notify.Watches())
	if err != nil {
		return err
	}
	svc.OnError = func(err error) { lg.Warnf("%s", err) }

	lock, err := svc.Watches.Lock(lockName, lockStale)
	if err != nil {
		if held, ok := errors.AsType[*storage.LockError](err); ok {
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
	stopBeat := heartbeat(lock, lg, func(err error) { stop(err) })
	defer stopBeat()

	listener, err := listenArgs(args)
	if err != nil {
		return err
	}
	defer listener.Close()
	loopback := isLoopback(listener.Addr())
	auth, err := listenerAuth(args, listener.Addr(), lg)
	if err != nil {
		return err
	}

	browser, err := web.NewBrowser(out)
	if err != nil {
		return err
	}
	jobManager := jobs.New(ctx)
	defer jobManager.Shutdown(10 * time.Second)
	events := NewEvents()
	go events.forwardWatches(ctx, svc)
	go events.forwardJobs(ctx, jobManager)
	go events.forwardLogs(ctx, log.Dir())

	var current atomic.Pointer[watch.Service]
	current.Store(svc)
	r := &runs{}
	address := serverAddress(listener.Addr(), args)
	started := time.Now()
	rl := &reloader{current: &current, log: lg}
	sched := newScheduler(rl.reload, lg, r)
	api := &API{
		Manager: func() watch.Manager { return current.Load() },
		Info:    watch.Status{Server: true, Address: address, Version: meta.Version, Started: started},
		Auth:    auth,
		Changed: sched.Wake,
		Log:     lg,
		Runs:    r,
		Browser: browser,
		Jobs:    jobManager,
		Events:  events,
		ctx:     ctx,
	}
	app, err := api.NewApp(AppOptions{Web: args.Web, Guard: auth.guard(listener.Addr())})
	if err != nil {
		return err
	}
	server := web.NewServer(ctx, app)

	dataDB := watch.DataDB()
	discovery := watch.DiscoveryPath(dataDB)
	if err := watch.WriteDiscovery(discovery, watch.Discovery{PID: os.Getpid(), Address: address, Started: started, Version: meta.Version, Data: dataDB, Key: auth.LocalKey}); err != nil {
		lg.Warnf("%s (the TUI and the CLI won't find this server)", err)
	}
	defer removeDiscovery(discovery, lg)

	extra := ""
	if args.Web {
		extra = ", web UI on " + address
	}
	listen := address
	if !loopback {
		listen = fmt.Sprintf("%s (%s)", listener.Addr(), address)
	}
	listen += " (" + auth.describe() + ")"
	lg.Infof("mangal serve %s on %s%s, data %s (pid %d)", meta.Version, listen, extra, dataDB, os.Getpid())

	schedDone := make(chan struct{})
	go func() {
		defer close(schedDone)
		sched.Run(ctx)
	}()
	if every, err := time.ParseDuration(config.Health.SourceCheckInterval.Get()); err == nil && every > 0 {
		go api.newSourceCheck(every).loop(ctx)
	}

	err = web.Serve(ctx, server, listener, func() {
		// stopped by the caller (its cause may be the signal), not a failure
		if parent.Err() != nil {
			lg.Infof("stopping")
		}
		// a failed server stops the scheduler too: systemd restarts the whole thing
		stop(nil)
		<-schedDone
		api.Wait()
	})
	if cause := context.Cause(ctx); err == nil && parent.Err() == nil && cause != context.Canceled {
		err = cause
	}
	if err != nil {
		lg.Errorf("%s", err)
	}
	lg.Infof("stopped")
	return err
}

// listenerAuth is the Auth of a server listening on addr: the users and sessions of the
// data db, the API tokens, a new local key (the discovery file has it).
func listenerAuth(args Args, addr net.Addr, lg logger) (*Auth, error) {
	auth, err := newAuth(args)
	if err != nil {
		return nil, err
	}
	switch auth.Mode {
	case AuthNone:
		lg.Warnf("serve.auth is none: every request that reaches %s may do everything (only behind something that authenticates)", addr)
	default:
		if st, err := cache.Storage(); err == nil {
			if users, err := st.Users().List(); err == nil && len(users) == 0 {
				lg.Warnf("no users yet: the web UI and OPDS need one, `mangal user add <name>` makes it")
			}
		}
	}
	return auth, nil
}

// newAuth is the server's Auth from its args.
func newAuth(args Args) (*Auth, error) {
	mode, err := authMode(args.Auth)
	if err != nil {
		return nil, err
	}
	proxies, err := ParseProxies(args.TrustedProxies)
	if err != nil {
		return nil, err
	}
	key, err := NewToken()
	if err != nil {
		return nil, err
	}
	auth := &Auth{Mode: mode, Tokens: cache.ServeTokens, LocalKey: key, TrustedProxies: proxies}
	if mode == AuthLogin {
		st, err := cache.Storage()
		if err != nil {
			return nil, err
		}
		auth.Users = st.Users()
		auth.Sessions = NewSessions(st.Sessions(), args.SessionLifetime)
	}
	return auth, nil
}

// authMode is serve.auth's mode; token (before the logins) is login.
func authMode(mode string) (string, error) {
	switch strings.ToLower(cmp.Or(mode, AuthLogin)) {
	case AuthLogin, "token":
		return AuthLogin, nil
	case AuthNone:
		return AuthNone, nil
	}
	return "", fmt.Errorf("serve.auth: %q isn't login or none", mode)
}

// guard is what the requests on addr have to pass first: the origin check, the host check
// only without logins (DNS rebinding).
func (a *Auth) guard(addr net.Addr) web.GuardOptions {
	return web.GuardOptions{Addr: addr, TrustedProxies: a.TrustedProxies, AnyHost: a.Mode != AuthNone}
}

// describe says how the requests are authenticated, for the start line.
func (a *Auth) describe() string {
	if a.Mode == AuthNone {
		return "no authentication"
	}
	return "login required"
}

// listen is net.Listen, swapped in tests.
var listen = net.Listen

// listenArgs listens on the args' host and port, with TLS when its files are set.
func listenArgs(args Args) (net.Listener, error) {
	listener, err := listen("tcp", net.JoinHostPort(args.Host, fmt.Sprint(args.Port)))
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	if args.TLSCert == "" && args.TLSKey == "" {
		return listener, nil
	}
	tlsListener, err := tlsutil.TLSListener(listener, args.TLSCert, args.TLSKey)
	if err != nil {
		listener.Close()
		return nil, err
	}
	return tlsListener, nil
}

// serverAddress is the address the server is reached at on this machine, https with TLS.
func serverAddress(addr net.Addr, args Args) string {
	address := watch.LocalAddress(addr)
	if args.TLSCert != "" {
		address = "https://" + strings.TrimPrefix(address, "http://")
	}
	return address
}

// heartbeatEvery is how often the lock's heartbeat is written.
var heartbeatEvery = lockStale / 4

// heartbeat keeps the lock alive until stopped. A lost lock (another server took it
// over) calls lost: two servers can't share a data db.
func heartbeat(lock *storage.Lock, lg logger, lost func(error)) (stop func()) {
	return lock.KeepAlive(heartbeatEvery, func(err error) {
		lost(fmt.Errorf("%w: another mangal serve took over %s", err, watch.DataDB()))
	}, func(err error) { lg.Warnf("%s", err) })
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
	config.Watch.Schedule.Key, config.Watch.Jitter.Key, config.Watch.GapSchedule.Key, config.Watch.Gaps.Key,
	config.Watch.ParallelProviders.Key, config.Watch.Backoff.Key, config.Watch.Notify.Key,
	config.Jobs.Check.Providers.Key,
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
	schedule := func(key string, def cron.Schedule) cron.Schedule {
		s, ok := values[key].(string)
		if !ok {
			return def
		}
		sched, err := watch.ParseSchedule(s)
		if err != nil {
			return def
		}
		return sched
	}
	opts.Schedule = schedule(config.Watch.Schedule.Key, opts.Schedule)
	opts.Jitter = duration(config.Watch.Jitter.Key, opts.Jitter)
	opts.GapSchedule = schedule(config.Watch.GapSchedule.Key, opts.GapSchedule)
	if b, ok := values[config.Watch.Gaps.Key].(bool); ok {
		opts.Gaps = b
	}
	opts.ParallelProviders = integer(config.Watch.ParallelProviders.Key, opts.ParallelProviders)
	opts.Backoff = integer(config.Watch.Backoff.Key, opts.Backoff)
	if s, ok := values[config.Watch.Notify.Key].(string); ok {
		opts.Notify = s
	}
	if raw, ok := values[config.Jobs.Check.Providers.Key].(map[string]any); ok {
		p := config.ProviderSettings{}
		for id, v := range raw {
			p[id], _ = v.(string)
		}
		opts.ProviderSchedules = watch.ProviderSchedules(p)
	}
	return opts
}

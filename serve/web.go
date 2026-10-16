package serve

import (
	"context"
	"io"
	"time"

	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/meta"
	"github.com/luevano/mangal/v5/notify"
	"github.com/luevano/mangal/v5/web"
	"github.com/skratchdot/open-golang/open"
)

// RunWeb serves the web UI and the API v1 like `mangal serve --web`, without the
// scheduler (`mangal web`): the watches are the running server's when there is one
// (watch.Connect, like the TUI), else this process's; downloads and scans are this
// process's jobs. The auth is serve's (args). It stops when ctx is cancelled.
func RunWeb(ctx context.Context, args Args, openIt bool, out io.Writer) error {
	lg := newLogger(out)
	listener, err := listenArgs(args)
	if err != nil {
		return err
	}
	defer listener.Close()
	auth, err := listenerAuth(args, listener.Addr(), lg)
	if err != nil {
		return err
	}
	m, err := watch.Connect(ctx, notify.Watches())
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
	go events.forwardWatches(ctx, m)
	go events.forwardJobs(ctx, jobManager)
	go events.forwardLogs(ctx, log.Dir())

	address := serverAddress(listener.Addr(), args)
	api := &API{
		Manager: func() watch.Manager { return m },
		Info:    watch.Status{Server: true, Address: address, Version: meta.Version, Started: time.Now()},
		Auth:    auth,
		Log:     lg,
		Runs:    &runs{},
		Browser: browser,
		Jobs:    jobManager,
		Events:  events,
		ctx:     ctx,
	}
	app, err := api.NewApp(AppOptions{Web: true, Guard: web.GuardOptions{Addr: listener.Addr(),
		AllowedHosts: args.AllowedHosts, AllowedOrigins: args.AllowedOrigins, TrustedProxies: auth.TrustedProxies}})
	if err != nil {
		return err
	}
	watches := "this process's watches"
	if _, remote := m.(*watch.Remote); remote {
		watches = "the watches of the mangal serve running"
	}
	lg.Infof("mangal web %s on %s (%s), %s", meta.Version, address, auth.describe(), watches)
	if openIt {
		if err := open.Start(address); err != nil {
			log.Warn("open %s: %s", address, err)
		}
	}
	return web.Serve(ctx, web.NewServer(ctx, app), listener, api.Wait)
}

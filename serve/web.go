package serve

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/meta"
	"github.com/luevano/mangal/v5/notify"
	"github.com/luevano/mangal/v5/web"
	"github.com/skratchdot/open-golang/open"
)

// RunWeb is `mangal web`: the web UI of the running `mangal serve` of this config when
// there's one (opened with openIt), else the web UI and the API v1 served like `mangal
// serve` (on its address, with its auth) without the scheduler: the watches are this
// process's, downloads and scans its jobs. It stops when ctx is cancelled.
func RunWeb(ctx context.Context, args Args, openIt bool, out io.Writer) error {
	lg := newLogger(out)
	if remote, d, err := watch.Find(ctx, watch.DataDB()); err != nil {
		return err
	} else if remote != nil {
		if !serverHasUI(ctx, remote) {
			return fmt.Errorf("mangal serve runs on %s without the web UI: set serve.web to true and restart it", d.Address)
		}
		lg.Infof("the web UI of the mangal serve running: %s", d.Address)
		if openIt {
			if err := open.Start(d.Address); err != nil {
				log.Warn("open %s: %s", d.Address, err)
			}
		}
		return nil
	}
	listener, err := listenArgs(args)
	if err != nil {
		return err
	}
	defer listener.Close()
	auth, err := listenerAuth(args, listener.Addr(), lg)
	if err != nil {
		return err
	}
	m, err := watch.New(notify.Watches())
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
	app, err := api.NewApp(AppOptions{Web: true, Guard: auth.guard(listener.Addr())})
	if err != nil {
		return err
	}
	lg.Infof("mangal web %s on %s (%s), the watches aren't checked on schedule (mangal serve does that)", meta.Version, address, auth.describe())
	if openIt {
		if err := open.Start(address); err != nil {
			log.Warn("open %s: %s", address, err)
		}
	}
	return web.Serve(ctx, web.NewServer(ctx, app), listener, api.Wait)
}

// serverHasUI is true when the server answers its web UI (serve.web).
func serverHasUI(ctx context.Context, remote *watch.Remote) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(remote.Address, "/")+"/", nil)
	if err != nil {
		return false
	}
	res, err := remote.Client.Do(req)
	if err != nil {
		return false
	}
	res.Body.Close()
	return res.StatusCode == http.StatusOK
}

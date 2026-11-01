package tui

import (
	"context"
	"errors"

	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/remote"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/jobs"
	configwindow "github.com/luevano/mangal/v5/tui/state/config"
	"github.com/luevano/mangal/v5/tui/state/connections"
	"github.com/luevano/mangal/v5/tui/state/downloads"
	"github.com/luevano/mangal/v5/tui/state/home"
	"github.com/luevano/mangal/v5/tui/state/logs"
	"github.com/luevano/mangal/v5/tui/state/watches"
	"github.com/luevano/mangal/v5/util"
	"github.com/luevano/mangal/v5/util/cache"
)

// localWatches are this machine's watches, as a server's replace them.
var localWatches = watches.Connect

// errDetached is what no screen should get: the connection doesn't work, only the picker's up.
var errDetached = errors.New("not connected: pick a connection")

// Run runs the TUI on t until the user quits or ctx is cancelled. A switch of connection (the
// picker) ends the screens of one backend and starts over at home on the other; it's saved,
// unless a flag or the variable chose this run's. A connection that doesn't work opens at the
// picker: never another backend, a user who believes they're on a server must not change local
// data.
func Run(ctx context.Context, t remote.Target) error {
	var notices []string
	for {
		err := runOn(ctx, t, notices)
		sw, ok := errors.AsType[*base.SwitchError](err)
		if !ok {
			return err
		}
		t, notices = switchTo(t, sw.Value)
	}
}

// switchTo is the target after a switch to value, saved when nothing overrides the key.
func switchTo(t remote.Target, value string) (remote.Target, []string) {
	next := remote.Target{Remote: value, Warn: t.Warn}
	if value == config.LocalConnection {
		next = remote.Target{Local: true, Warn: t.Warn}
	}
	if t.Remote != "" || t.Local {
		return next, []string{"On " + value + " for this session: --remote or --local chose this run, the saved connection stays."}
	}
	if env, set := config.EnvVar(config.Connection.Key); set {
		return next, []string{"On " + value + " for this session: " + env + " is set, the saved connection stays."}
	}
	if err := remote.UseConnection(value); err != nil {
		return next, []string{"On " + value + " for this session, it isn't saved: " + err.Error()}
	}
	return next, nil
}

// runOn runs the TUI once on t's backend, set up from scratch: nothing's carried over.
func runOn(ctx context.Context, t remote.Target, notices []string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if config.Server() != "" {
		s, err := cache.Settings()
		if err != nil {
			return err
		}
		if err := config.UseLocal(s); err != nil {
			return err
		}
	}
	base.CurrentRemote, base.CurrentActivity = nil, nil
	base.Core, watches.Connect = core.Default, localWatches
	setScreens()
	base.LocalHeader = len(config.Connections.Get()) > 0 || localServe()

	url, err := t.Address()
	var rc *remote.Client
	if err == nil {
		rc, err = remote.Connect(ctx, t)
	}
	if err == nil && rc != nil {
		err = UseServer(ctx, rc)
	}
	if err != nil {
		return runDetached(ctx, url, err, notices)
	}
	if rc != nil {
		base.CurrentActivity = jobs.StartRemote(ctx, rc.Jobs())
		return base.Run(ctx, home.New(), notices...)
	}
	notices = append(notices, cache.Notices()...)
	if notice := anilist.LoginNotice(); notice != "" {
		notices = append(notices, notice)
	}
	base.CurrentActivity = jobs.Start(ctx)
	return base.Run(ctx, home.New(), notices...)
}

// runDetached is the picker alone, with why the connection doesn't work: nothing else opens.
func runDetached(ctx context.Context, url string, why error, notices []string) error {
	log.Warn("tui: %s", why)
	base.Core = func() (*core.Core, error) { return nil, errDetached }
	watches.Connect = func(context.Context) (watch.Manager, error) { return nil, errDetached }
	base.NewConfigWindow, base.NewDownloads, base.NewLogs, base.NewConnections = nil, nil, nil, nil
	// none's in use: not local either
	if url == "" {
		url = "?"
	}
	return base.Run(ctx, connections.New(url, util.ErrorTree(why)), notices...)
}

// localServe is true when a mangal serve of this config has its discovery file.
func localServe() bool {
	data := watch.DataDB()
	d, found, err := watch.ReadDiscovery(watch.DiscoveryPath(data))
	return err == nil && found && d.Data == data
}

func setScreens() {
	base.NewConfigWindow = configwindow.New
	base.NewDownloads = downloads.New
	base.NewLogs = logs.New
	base.NewConnections = func() base.State {
		current := ""
		if r := base.CurrentRemote; r != nil {
			current = r.URL
		}
		return connections.New(current, "")
	}
}

// errRemote is this machine's core asked for on a remote server: no screen should.
var errRemote = errors.New("this machine's library isn't used on a remote mangal serve")

// UseServer makes rc's mangal serve the TUI's backend: base.Services are its, base.Core and
// this machine's watches answer nothing.
func UseServer(ctx context.Context, rc *remote.Client) error {
	st, err := rc.Status(ctx)
	if err != nil {
		return err
	}
	me, err := rc.Me(ctx)
	if err != nil {
		return err
	}
	svc := rc.Services()
	r := &base.Remote{URL: rc.URL, Version: "?", Features: st.Features, Perms: st.Scopes, Who: string(me.Kind) + " " + me.Name,
		ShowAll: !me.HideUnavailable, Services: svc, Loaders: rc.Loaders, Client: rc}
	if st.Version != nil {
		r.Version = *st.Version
	}
	if r.MetadataSources, err = rc.MetadataSources(ctx); err != nil {
		log.Warn("tui: the metadata sources of %s: %s", r.Name(), err)
	}
	var since uint64
	if st.EventSeq != nil {
		since = *st.EventSeq
	}
	r.Events = func(ctx context.Context, fn func([]string)) {
		err := rc.Events(ctx, []string{"library", "history", "settings", "watches"}, since, func(e remote.Event) {
			if rev, ok := serverTopics[e.Topic]; ok && e.Kind == "changed" {
				if rev == storage.RevSettings {
					r.RefreshConfig(ctx)
				}
				fn([]string{rev})
			}
		})
		if err != nil {
			log.Warn("tui: the server's events: %s", err)
		}
	}
	base.CurrentRemote = r
	base.Core = func() (*core.Core, error) { return nil, errRemote }
	watches.Connect = svc.Watches
	return nil
}

// serverTopics are the topics of the server's "changed" events as the data database's.
var serverTopics = map[string]string{
	"library": storage.RevLibrary, "history": storage.RevHistory, "settings": storage.RevSettings, "watches": storage.RevWatches,
}

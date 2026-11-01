package tui

import (
	"context"
	"errors"

	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/remote"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/jobs"
	configwindow "github.com/luevano/mangal/v5/tui/state/config"
	"github.com/luevano/mangal/v5/tui/state/downloads"
	"github.com/luevano/mangal/v5/tui/state/home"
	"github.com/luevano/mangal/v5/tui/state/logs"
	"github.com/luevano/mangal/v5/tui/state/watches"
	"github.com/luevano/mangal/v5/util/cache"
)

// Run runs the TUI until the user quits or ctx is cancelled.
func Run(ctx context.Context) error {
	notices := cache.Notices()
	if notice := anilist.LoginNotice(); notice != "" {
		notices = append(notices, notice)
	}
	setScreens()
	base.CurrentActivity = jobs.Start(ctx)
	return base.Run(ctx, home.New(), notices...)
}

func setScreens() {
	base.NewConfigWindow = configwindow.New
	base.NewDownloads = downloads.New
	base.NewLogs = logs.New
}

// errRemote is this machine's core asked for on a remote server: no screen should.
var errRemote = errors.New("this machine's library isn't used on a remote mangal serve")

// RunRemote runs the TUI on the mangal serve of rc (checked: remote.Connect): its
// services, jobs and events; this machine keeps the TUI's own settings, log and search
// histories.
func RunRemote(ctx context.Context, rc *remote.Client) error {
	if err := UseServer(ctx, rc); err != nil {
		return err
	}
	setScreens()
	base.CurrentActivity = jobs.StartRemote(ctx, rc.Jobs())
	return base.Run(ctx, home.New())
}

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

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

// errRemote is a screen that needs this machine's core on a remote server.
var errRemote = errors.New("not on a remote mangal serve yet (mangal without --remote does it)")

// RunRemote runs the TUI on the mangal serve of rc (checked: remote.Connect): its
// services, jobs and events; this machine keeps the TUI's own settings, log and search
// histories.
func RunRemote(ctx context.Context, rc *remote.Client) error {
	st, err := rc.Status(ctx)
	if err != nil {
		return err
	}
	me, err := rc.Me(ctx)
	if err != nil {
		return err
	}
	svc := rc.Services()
	r := &base.Remote{URL: rc.URL, Version: "?", Features: st.Features, Perms: st.Scopes, Who: string(me.Kind) + " " + me.Name, Services: svc}
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
	setScreens()
	base.CurrentActivity = jobs.StartRemote(ctx, rc.Jobs())
	return base.Run(ctx, home.New())
}

// serverTopics are the topics of the server's "changed" events as the data database's.
var serverTopics = map[string]string{
	"library": storage.RevLibrary, "history": storage.RevHistory, "settings": storage.RevSettings, "watches": storage.RevWatches,
}

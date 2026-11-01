package accounts

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/remote"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/huhform"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/luevano/mangal/v5/web/apiv1"
	"github.com/skratchdot/open-golang/open"
)

// A remote TUI logs its user into their trackers on the server, as the web UI: AniList and
// MyAnimeList answer an address to open, what its page ends on is pasted; Kitsu takes a
// name and password. The other accounts are set on the server's machine.

// trackerStartedMsg is a login started, waiting for its paste.
type trackerStartedMsg struct{ source, url string }

// trackerDoneMsg reloads the accounts and says what changed.
type trackerDoneMsg struct{ notice string }

// remoteTracker is true on a remote server's row of a tracker the user is logged into.
func (s *state) remoteTracker() bool {
	entry, ok := s.selected()
	return ok && base.CurrentRemote != nil && entry.Metadata != 0 && entry.User != ""
}

func (s *state) trackerLogin(ctx context.Context, r *base.Remote, entry core.Account) tea.Cmd {
	source := remote.TrackerSource(entry.Metadata)
	switch {
	case source == "":
		return base.Notify(entry.Service + " is set on the server's machine (mangal accounts there)")
	case entry.Metadata == metadata.IDSourceKitsu:
		s.target = source
		name := huhform.Text("Name", "", "", required)
		password := huhform.Secret("Password", "", "", required)
		return s.openForm(formTrackerPassword, name, password)
	}
	return base.Load(ctx, "Starting the login", func(ctx context.Context) (trackerStartedMsg, error) {
		url, err := r.Client.TrackerLogin(ctx, source, apiv1.TrackerLogin{})
		return trackerStartedMsg{source: source, url: url}, err
	}, func(m trackerStartedMsg) tea.Msg { return m })
}

func (s *state) trackerLoginCmd(ctx context.Context, source string, login apiv1.TrackerLogin) tea.Cmd {
	rc := base.CurrentRemote.Client
	return base.Load(ctx, "Logging in", func(ctx context.Context) (trackerDoneMsg, error) {
		_, err := rc.TrackerLogin(ctx, source, login)
		return trackerDoneMsg{notice: "Logged in"}, err
	}, func(m trackerDoneMsg) tea.Msg { return m })
}

func (s *state) trackerLogout(ctx context.Context) tea.Cmd {
	entry, _ := s.selected()
	source, rc := remote.TrackerSource(entry.Metadata), base.CurrentRemote.Client
	return base.Load(ctx, "Logging out", func(ctx context.Context) (trackerDoneMsg, error) {
		return trackerDoneMsg{notice: fmt.Sprintf("Logged out of %s", entry.Service)}, rc.TrackerLogout(ctx, source)
	}, func(m trackerDoneMsg) tea.Msg { return m })
}

// updateTracker handles the tracker logins' messages, false when msg isn't one.
func (s *state) updateTracker(ctx context.Context, msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case trackerStartedMsg:
		s.target, s.loginURL = msg.source, msg.url
		form := s.openForm(formTrackerPaste, huhform.Text("Paste", "", "the code, or the address the page sends you to", required))
		return tea.Batch(form, func() tea.Msg {
			// the address is on the screen and copied if no browser opens
			_ = open.Start(msg.url)
			return base.NotificationMsg{Message: util.CopyNotice(msg.url, "Copied the login address")}
		}), true
	case trackerDoneMsg:
		return tea.Batch(base.Notify(msg.notice), s.load(ctx, false)), true
	}
	return nil, false
}

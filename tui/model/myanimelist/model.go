// Package myanimelist is the MyAnimeList login screen (mangal myanimelist, and from the
// accounts).
package myanimelist

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/auth/oauth"
	"github.com/luevano/mangal/v5/client/myanimelist"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
)

type phase int

const (
	checking phase = iota
	// notConfigured is without a client id
	notConfigured
	loggedIn
	// saved lists the saved logins
	saved
	// form is a new login
	form
)

var _ base.State = (*state)(nil)

type state struct {
	phase phase
	user  metadata.User
	users []string

	list *list.Model
	size base.Size
	keys keyMap
}

// statusMsg is the login state, after checking it or a login action.
type statusMsg struct {
	notConfigured bool
	user          metadata.User
	users         []string
	notice        string
	// form opens the new login (e.g. the saved one expired)
	form bool
}

// New is the MyAnimeList login screen.
func New() base.State {
	l := list.New(1, 0, "user", "users", []string{}, func(u string) _list.DefaultItem { return item(u) })
	l.SetAccentColor(color.MyAnimeList)
	l.KeyMap.List.Filter.SetEnabled(false)
	l.KeyMap.Reverse.SetEnabled(false)
	l.KeyMap.List.GoToStart.SetEnabled(false)
	l.KeyMap.List.GoToEnd.SetEnabled(false)
	l.KeyMap.List.NextPage.SetEnabled(false)
	l.KeyMap.List.PrevPage.SetEnabled(false)

	s := &state{list: l, keys: newKeyMap()}
	s.updateKeybinds()
	return s
}

func (s *state) Backable() bool  { return !s.keys.back.Enabled() }
func (s *state) Inputting() bool { return false }

func (s *state) Title() base.Title {
	return base.Title{Text: "MyAnimeList login", Background: color.MyAnimeList, Foreground: color.Bright}
}

func (s *state) Subtitle() string {
	if s.phase == form {
		return "the login opens in the browser, for one on another machine use mangal myanimelist login"
	}
	return ""
}

func (s *state) Status() string { return "" }

func (s *state) KeyMap() help.KeyMap {
	if s.phase == saved {
		return base.CombinedKeyMap(&s.keys, s.list.KeyMap)
	}
	return &s.keys
}

func (s *state) Resize(size base.Size) tea.Cmd {
	s.size = size
	size.Height = min(size.Height, max(1, len(s.users)))
	return s.list.Resize(size)
}

func (s *state) Init(ctx context.Context) tea.Cmd {
	return base.Load(ctx, "Checking the MyAnimeList login", func(ctx context.Context) (statusMsg, error) {
		if err := myanimelist.EnsureLogin(ctx); err != nil && !errors.Is(err, myanimelist.ErrNotConfigured) {
			// still usable, e.g. to log in again
			log.Warn("myanimelist login: %s", err)
		}
		return status(ctx)
	}, toMsg)
}

func toMsg(m statusMsg) tea.Msg { return m }

// status reads the login state, nothing is requested.
func status(context.Context) (statusMsg, error) {
	m, err := myanimelist.MyAnimeList()
	if errors.Is(err, myanimelist.ErrNotConfigured) {
		return statusMsg{notConfigured: true}, nil
	}
	if err != nil {
		return statusMsg{}, err
	}
	users, err := myanimelist.Users()
	if err != nil {
		return statusMsg{}, err
	}
	var user metadata.User
	if m.Authenticated() {
		user = m.User()
	}
	return statusMsg{user: user, users: users}, nil
}

func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case statusMsg:
		return s.setStatus(msg)
	case tea.KeyMsg:
		if cmd, ok := s.updateKey(ctx, msg); ok {
			return cmd
		}
	}
	if s.phase == saved {
		return s.list.Update(msg)
	}
	return nil
}

func (s *state) setStatus(msg statusMsg) tea.Cmd {
	s.user, s.users = msg.user, msg.users
	items := make([]_list.Item, len(s.users))
	for i, u := range s.users {
		items[i] = item(u)
	}
	cmd := tea.Batch(s.list.SetItems(items), s.Resize(s.size))

	switch {
	case msg.notConfigured:
		s.phase = notConfigured
	case s.user != nil:
		s.phase = loggedIn
	case msg.form || len(s.users) == 0:
		s.phase = form
	default:
		s.phase = saved
	}
	s.updateKeybinds()
	if msg.notice != "" {
		cmd = tea.Batch(cmd, base.NotifyWithDuration(msg.notice, noticeDuration))
	}
	return cmd
}

func (s *state) updateKey(ctx context.Context, msg tea.KeyMsg) (tea.Cmd, bool) {
	selected, _ := s.list.SelectedItem().(item)
	switch {
	case key.Matches(msg, s.keys.back):
		s.phase = saved
	case key.Matches(msg, s.keys.login):
		if base.Busy(ctx) {
			return nil, true
		}
		if s.phase == form {
			return s.loginNew(ctx), true
		}
		return s.loginSaved(ctx, string(selected)), true
	case key.Matches(msg, s.keys.logout):
		if base.Busy(ctx) {
			return nil, true
		}
		return s.logout(ctx), true
	case key.Matches(msg, s.keys.new):
		s.phase = form
	case key.Matches(msg, s.keys.forget):
		if base.Busy(ctx) {
			return nil, true
		}
		return s.forget(ctx, string(selected)), true
	default:
		return nil, false
	}
	s.updateKeybinds()
	return nil, true
}

func (s *state) loginSaved(ctx context.Context, user string) tea.Cmd {
	if user == "" {
		return nil
	}
	return base.Load(ctx, fmt.Sprintf("Logging in %q", user), func(ctx context.Context) (statusMsg, error) {
		_, err := myanimelist.LoginSaved(ctx, user)
		var expired *myanimelist.ExpiredError
		notice := fmt.Sprintf("Logged in as %q", user)
		form := false
		switch {
		case errors.Is(err, myanimelist.ErrUserNotFound):
			notice = fmt.Sprintf("The login of %q is gone, removed it", user)
		case errors.As(err, &expired):
			notice, form = expired.Error(), true
		case err != nil:
			return statusMsg{}, err
		}
		msg, err := status(ctx)
		msg.notice, msg.form = notice, form
		return msg, err
	}, toMsg)
}

func (s *state) loginNew(ctx context.Context) tea.Cmd {
	return base.Load(ctx, "Waiting for the login in the browser", func(ctx context.Context) (statusMsg, error) {
		user, err := myanimelist.LoginNew(ctx)
		if err != nil {
			return statusMsg{}, fmt.Errorf("myanimelist login: %w", err)
		}
		msg, err := status(ctx)
		msg.notice = fmt.Sprintf("Logged in as %q", user.Name())
		return msg, err
	}, toMsg)
}

func (s *state) logout(ctx context.Context) tea.Cmd {
	return base.Load(ctx, "", func(ctx context.Context) (statusMsg, error) {
		if err := myanimelist.Logout(); err != nil {
			return statusMsg{}, fmt.Errorf("myanimelist logout: %w", err)
		}
		msg, err := status(ctx)
		msg.notice = "Logged out until the next login"
		return msg, err
	}, toMsg)
}

func (s *state) forget(ctx context.Context, user string) tea.Cmd {
	if user == "" {
		return nil
	}
	return base.Load(ctx, "", func(ctx context.Context) (statusMsg, error) {
		if err := myanimelist.Forget(user); err != nil {
			return statusMsg{}, err
		}
		msg, err := status(ctx)
		msg.notice = fmt.Sprintf("Forgot the login of %q", user)
		return msg, err
	}, toMsg)
}

// updateKeybinds enables the keys of the phase.
func (s *state) updateKeybinds() {
	s.keys.back.SetEnabled(s.phase == form && len(s.users) != 0)
	s.keys.login.SetEnabled(s.phase == saved || s.phase == form)
	s.keys.logout.SetEnabled(s.phase == loggedIn)
	s.keys.new.SetEnabled(s.phase == saved)
	s.keys.forget.SetEnabled(s.phase == saved && len(s.users) != 0)
}

func (s *state) View() string {
	switch s.phase {
	case checking:
		return ""
	case notConfigured:
		return lipgloss.JoinVertical(lipgloss.Left,
			"MyAnimeList needs the client ID of a MyAnimeList API client:",
			"register one at https://myanimelist.net/apiconfig (App Type \"other\",",
			"App Redirect URL "+oauth.MyAnimeListServerCallbackURL+"),",
			"then set "+config.MyAnimeList.ClientID.Key+" to its ID.",
		)
	case loggedIn:
		return "Logged in as " + s.user.String()
	case saved:
		return lipgloss.JoinVertical(lipgloss.Left, "Saved logins", " ", s.list.View())
	default:
		return "i opens the MyAnimeList login in the browser (" + oauth.MyAnimeListServerLoginURL + ")"
	}
}

// item is a saved user.
type item string

func (i item) FilterValue() string { return string(i) }
func (i item) Title() string       { return string(i) }
func (i item) Description() string { return "" }

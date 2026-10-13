// Package anilist is the Anilist login screen (mangal anilist, and from the accounts).
package anilist

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
)

type phase int

const (
	checking phase = iota
	loggedIn
	// saved lists the saved logins
	saved
	// form is a new login
	form
)

type field int

const (
	idField field = iota
	secretField
	fieldCount
)

var _ base.State = (*state)(nil)

type state struct {
	phase phase
	user  metadata.User
	users []string

	list    *list.Model
	inputs  [fieldCount]textinput.Model
	current field
	editing bool

	size         base.Size
	selectCursor string
	styles       styles
	keys         keyMap
}

// statusMsg is the login state, after checking it or a login action.
type statusMsg struct {
	// id and secret are the API client of the last new login
	id, secret string
	user       metadata.User
	users      []string
	notice     string
	// form opens the new login (e.g. the saved one expired)
	form bool
}

// New is the Anilist login screen.
func New() base.State {
	_styles := defaultStyles()
	input := textinput.New()
	input.Width = 60
	input.PromptStyle = _styles.prompt
	input.TextStyle = _styles.text

	id, secret := input, input
	id.Prompt = "ID:     "
	secret.Prompt = "Secret: "
	secret.Placeholder = "(optional)"
	secret.EchoMode = textinput.EchoPassword

	l := list.New(1, 0, "user", "users", []string{}, func(u string) _list.DefaultItem { return item(u) })
	l.SetAccentColor(color.Anilist)
	// no filtering: its typing would also be the screen's keys
	l.SetFilteringEnabled(false)
	l.KeyMap.Reverse.SetEnabled(false)
	l.KeyMap.List.GoToStart.SetEnabled(false)
	l.KeyMap.List.GoToEnd.SetEnabled(false)
	l.KeyMap.List.NextPage.SetEnabled(false)
	l.KeyMap.List.PrevPage.SetEnabled(false)

	s := &state{
		list:         l,
		inputs:       [fieldCount]textinput.Model{id, secret},
		selectCursor: _styles.prompt.Render(icon.Item.Raw()),
		styles:       _styles,
		keys:         newKeyMap(),
	}
	s.updateKeybinds()
	return s
}

func (s *state) Key() string { return "login/anilist" }

func (s *state) Backable() bool {
	return !s.editing && !s.keys.back.Enabled()
}

func (s *state) Inputting() bool { return s.editing || s.list.Filtering() }

func (s *state) Title() base.Title {
	return base.Title{Text: "Anilist login", Background: color.Anilist, Foreground: color.Bright}
}

func (s *state) Subtitle() string {
	if s.phase == form {
		return "the id and secret of an Anilist API client, the login opens in the browser (none here? mangal anilist login)"
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
	return tea.Batch(textinput.Blink, base.Load(ctx, "Checking the Anilist login", func(ctx context.Context) (statusMsg, error) {
		if err := anilist.EnsureLogin(ctx); err != nil {
			// still usable, e.g. to log in again
			log.Warn("anilist login: %s", err)
		}
		return status(ctx)
	}, toMsg))
}

func toMsg(m statusMsg) tea.Msg { return m }

// status reads the login state, nothing is requested.
func status(context.Context) (statusMsg, error) {
	ani, err := anilist.Anilist()
	if err != nil {
		return statusMsg{}, err
	}
	users, err := anilist.Users()
	if err != nil {
		return statusMsg{}, err
	}
	var user metadata.User
	if ani.Authenticated() {
		user = ani.User()
	}
	id, secret, err := anilist.Client()
	if err != nil {
		return statusMsg{}, err
	}
	return statusMsg{id: id, secret: secret, user: user, users: users}, nil
}

func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case statusMsg:
		return s.setStatus(msg)
	case tea.KeyMsg:
		if s.editing {
			return s.updateEditing(msg)
		}
		if cmd, ok := s.updateKey(ctx, msg); ok {
			return cmd
		}
	}

	switch {
	case s.phase == form && s.editing:
		var cmd tea.Cmd
		s.inputs[s.current], cmd = s.inputs[s.current].Update(msg)
		return cmd
	case s.phase == saved:
		return s.list.Update(msg)
	}
	return nil
}

func (s *state) setStatus(msg statusMsg) tea.Cmd {
	s.user, s.users = msg.user, msg.users
	if s.inputs[idField].Value() == "" && s.inputs[secretField].Value() == "" {
		s.inputs[idField].SetValue(msg.id)
		s.inputs[secretField].SetValue(msg.secret)
	}
	items := make([]_list.Item, len(s.users))
	for i, u := range s.users {
		items[i] = item(u)
	}
	cmd := tea.Batch(s.list.SetItems(items), s.Resize(s.size))

	switch {
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
	case key.Matches(msg, s.keys.up):
		s.current = (s.current + fieldCount - 1) % fieldCount
	case key.Matches(msg, s.keys.down):
		s.current = (s.current + 1) % fieldCount
	case key.Matches(msg, s.keys.edit):
		s.editing = true
		s.inputs[s.current].CursorEnd()
		s.updateKeybinds()
		return s.inputs[s.current].Focus(), true
	case key.Matches(msg, s.keys.clear):
		s.inputs[s.current].SetValue("")
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

func (s *state) updateEditing(msg tea.KeyMsg) tea.Cmd {
	if key.Matches(msg, s.keys.confirm) || key.Matches(msg, s.keys.cancel) {
		s.editing = false
		s.inputs[s.current].Blur()
		s.updateKeybinds()
		return nil
	}
	var cmd tea.Cmd
	s.inputs[s.current], cmd = s.inputs[s.current].Update(msg)
	s.updateKeybinds()
	return cmd
}

func (s *state) loginSaved(ctx context.Context, user string) tea.Cmd {
	if user == "" {
		return nil
	}
	return base.Load(ctx, fmt.Sprintf("Logging in %q", user), func(ctx context.Context) (statusMsg, error) {
		_, err := anilist.LoginSaved(ctx, user)
		var expired *anilist.ExpiredError
		notice := fmt.Sprintf("Logged in as %q", user)
		form := false
		switch {
		case errors.Is(err, anilist.ErrUserNotFound):
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
	id, secret := s.inputs[idField].Value(), s.inputs[secretField].Value()
	return base.Load(ctx, "Logging in, finish it in the browser", func(ctx context.Context) (statusMsg, error) {
		user, err := anilist.LoginNew(ctx, id, secret)
		if err != nil {
			return statusMsg{}, fmt.Errorf("anilist login: %w", err)
		}
		msg, err := status(ctx)
		msg.notice = fmt.Sprintf("Logged in as %q", user.Name())
		return msg, err
	}, toMsg)
}

func (s *state) logout(ctx context.Context) tea.Cmd {
	return base.Load(ctx, "", func(ctx context.Context) (statusMsg, error) {
		if err := anilist.Logout(); err != nil {
			return statusMsg{}, fmt.Errorf("anilist logout: %w", err)
		}
		msg, err := status(ctx)
		msg.notice = "Logged out (until the next login)"
		return msg, err
	}, toMsg)
}

func (s *state) forget(ctx context.Context, user string) tea.Cmd {
	if user == "" {
		return nil
	}
	return base.Load(ctx, "", func(ctx context.Context) (statusMsg, error) {
		if err := anilist.Forget(user); err != nil {
			return statusMsg{}, err
		}
		msg, err := status(ctx)
		msg.notice = fmt.Sprintf("Forgot the login of %q", user)
		return msg, err
	}, toMsg)
}

// updateKeybinds enables the keys of the phase.
func (s *state) updateKeybinds() {
	idle := !s.editing
	inForm := s.phase == form

	s.keys.back.SetEnabled(inForm && idle && len(s.users) != 0)
	s.keys.login.SetEnabled(idle && (s.phase == saved || inForm && s.inputs[idField].Value() != ""))
	s.keys.logout.SetEnabled(s.phase == loggedIn)
	s.keys.new.SetEnabled(s.phase == saved)
	s.keys.forget.SetEnabled(s.phase == saved && len(s.users) != 0)

	navigate := inForm && idle
	s.keys.up.SetEnabled(navigate)
	s.keys.down.SetEnabled(navigate)
	s.keys.edit.SetEnabled(navigate)
	s.keys.clear.SetEnabled(navigate)

	s.keys.confirm.SetEnabled(s.editing)
	s.keys.cancel.SetEnabled(s.editing)
}

func (s *state) View() string {
	switch s.phase {
	case checking:
		return ""
	case loggedIn:
		return "Logged in as " + s.user.String()
	case saved:
		return lipgloss.JoinVertical(lipgloss.Left, "Saved logins", " ", s.list.View())
	default:
		return lipgloss.JoinVertical(lipgloss.Left, s.viewField(idField), s.viewField(secretField))
	}
}

func (s *state) viewField(f field) string {
	if s.current == f {
		return s.styles.selected.Render(s.selectCursor + s.inputs[f].View())
	}
	return s.styles.field.Render(s.inputs[f].View())
}

// item is a saved user.
type item string

func (i item) FilterValue() string { return string(i) }
func (i item) Title() string       { return string(i) }
func (i item) Description() string { return "" }

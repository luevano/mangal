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
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/auth/oauth"
	"github.com/luevano/mangal/v5/client/myanimelist"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
)

type phase int

const (
	checking phase = iota
	// setup is the API client form, first without a client id
	setup
	loggedIn
	// saved lists the saved logins
	saved
	// form is a new login
	form
)

var _ base.State = (*state)(nil)

type field int

const (
	idField field = iota
	secretField
	fieldCount
)

type state struct {
	phase phase
	user  metadata.User
	users []string
	// configured is true with a saved client id
	configured bool
	// back is the phase esc returns to from the setup
	back phase

	list    *list.Model
	inputs  [fieldCount]textinput.Model
	current field

	size         base.Size
	prompt       lipgloss.Style
	selectCursor string
	keys         keyMap
}

// statusMsg is the login state, after checking it or a login action.
type statusMsg struct {
	notConfigured bool
	// id and secret are the saved API client
	id, secret string
	user       metadata.User
	users      []string
	notice     string
	// form opens the new login (e.g. the saved one expired)
	form bool
}

// New is the MyAnimeList login screen.
func New() base.State {
	l := list.New(1, 0, "user", "users", []string{}, func(u string) _list.DefaultItem { return item(u) })
	l.SetAccentColor(color.MyAnimeList)
	// no filtering: its typing would also be the screen's keys
	l.SetFilteringEnabled(false)
	l.KeyMap.Reverse.SetEnabled(false)
	l.KeyMap.List.GoToStart.SetEnabled(false)
	l.KeyMap.List.GoToEnd.SetEnabled(false)
	l.KeyMap.List.NextPage.SetEnabled(false)
	l.KeyMap.List.PrevPage.SetEnabled(false)

	prompt := style.Bold.Base.Foreground(color.MyAnimeList)
	input := textinput.New()
	input.Width = 60
	input.PromptStyle = prompt
	id, secret := input, input
	id.Prompt = "Client ID:     "
	secret.Prompt = "Client secret: "
	secret.Placeholder = "(only for App Types other than \"other\")"
	secret.EchoMode = textinput.EchoPassword

	s := &state{
		list:         l,
		inputs:       [fieldCount]textinput.Model{id, secret},
		prompt:       prompt,
		selectCursor: prompt.Render(icon.Item.Raw()),
		keys:         newKeyMap(),
	}
	s.updateKeybinds()
	return s
}

func (s *state) Key() string { return "login/myanimelist" }

func (s *state) Backable() bool  { return !s.keys.back.Enabled() && !s.keys.cancel.Enabled() }
func (s *state) Inputting() bool { return s.phase == setup || s.list.Filtering() }

func (s *state) Title() base.Title {
	return base.Title{Text: "MyAnimeList login", Background: color.MyAnimeList, Foreground: color.Bright}
}

func (s *state) Subtitle() string {
	switch {
	case s.phase == setup && s.configured:
		return "the MyAnimeList API client mangal logs in with, an empty client ID removes it"
	case s.phase == setup:
		return "the MyAnimeList API client mangal logs in with, saved with the logins"
	case s.phase == form:
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
	return tea.Batch(textinput.Blink, base.Load(ctx, "Checking the MyAnimeList login", func(ctx context.Context) (statusMsg, error) {
		if err := myanimelist.EnsureLogin(ctx); err != nil && !errors.Is(err, myanimelist.ErrNotConfigured) {
			// still usable, e.g. to log in again
			log.Warn("myanimelist login: %s", err)
		}
		return status(ctx)
	}, toMsg))
}

func toMsg(m statusMsg) tea.Msg { return m }

// status reads the login state, nothing is requested.
func status(context.Context) (statusMsg, error) {
	id, secret, err := myanimelist.Client()
	if err != nil {
		return statusMsg{}, err
	}
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
	return statusMsg{id: id, secret: secret, user: user, users: users}, nil
}

func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case statusMsg:
		return s.setStatus(msg)
	case editClientMsg:
		cmd := s.openSetup(msg.id, msg.secret)
		s.updateKeybinds()
		return cmd
	case tea.KeyMsg:
		if cmd, ok := s.updateKey(ctx, msg); ok {
			return cmd
		}
	}
	switch s.phase {
	case setup:
		var cmd tea.Cmd
		s.inputs[s.current], cmd = s.inputs[s.current].Update(msg)
		return cmd
	case saved:
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

	s.configured = !msg.notConfigured
	switch {
	case msg.notConfigured:
		cmd = tea.Batch(cmd, s.openSetup("", ""))
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
	case key.Matches(msg, s.keys.save):
		if base.Busy(ctx) {
			return nil, true
		}
		return s.saveClient(ctx), true
	case key.Matches(msg, s.keys.next):
		return s.focus(s.current + 1), true
	case key.Matches(msg, s.keys.prev):
		return s.focus(s.current + fieldCount - 1), true
	case key.Matches(msg, s.keys.cancel):
		s.phase = s.back
	case key.Matches(msg, s.keys.client):
		return s.editClient(), true
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

// openSetup shows the API client form with the saved client.
func (s *state) openSetup(id, secret string) tea.Cmd {
	if s.phase != setup {
		s.back = s.phase
	}
	s.phase = setup
	s.inputs[idField].SetValue(id)
	s.inputs[secretField].SetValue(secret)
	for i := range s.inputs {
		s.inputs[i].CursorEnd()
	}
	return s.focus(idField)
}

func (s *state) focus(f field) tea.Cmd {
	s.current = f % fieldCount
	for i := range s.inputs {
		s.inputs[i].Blur()
	}
	return s.inputs[s.current].Focus()
}

// editClientMsg opens the setup with the saved client.
type editClientMsg struct{ id, secret string }

// editClient reads the saved client, no request.
func (s *state) editClient() tea.Cmd {
	return func() tea.Msg {
		id, secret, err := myanimelist.Client()
		if err != nil {
			return err
		}
		return editClientMsg{id: id, secret: secret}
	}
}

func (s *state) saveClient(ctx context.Context) tea.Cmd {
	id, secret := s.inputs[idField].Value(), s.inputs[secretField].Value()
	return base.Load(ctx, "", func(ctx context.Context) (statusMsg, error) {
		if err := myanimelist.SetClient(id, secret); err != nil {
			return statusMsg{}, err
		}
		notice := "Removed the MyAnimeList API client"
		if id != "" {
			notice = "Saved the MyAnimeList API client"
			if err := myanimelist.EnsureLogin(ctx); err != nil {
				log.Warn("myanimelist login: %s", err)
			}
		}
		msg, err := status(ctx)
		msg.notice = notice
		return msg, err
	}, toMsg)
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
	inSetup := s.phase == setup
	s.keys.save.SetEnabled(inSetup)
	s.keys.next.SetEnabled(inSetup)
	s.keys.prev.SetEnabled(inSetup)
	s.keys.cancel.SetEnabled(inSetup && s.configured)
	s.keys.client.SetEnabled(s.phase == loggedIn || s.phase == saved || s.phase == form)
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
	case setup:
		return lipgloss.JoinVertical(lipgloss.Left,
			"MyAnimeList needs an API client of your own: register one at",
			"https://myanimelist.net/apiconfig (App Type \"other\", App Redirect URL",
			oauth.MyAnimeListServerCallbackURL+"), the client ID is on its page.",
			" ",
			s.viewField(idField),
			s.viewField(secretField),
		)
	case loggedIn:
		return "Logged in as " + s.user.String()
	case saved:
		return lipgloss.JoinVertical(lipgloss.Left, "Saved logins", " ", s.list.View())
	default:
		return s.keys.login.Help().Key + " opens the MyAnimeList login in the browser (" + oauth.MyAnimeListServerLoginURL + ")"
	}
}

func (s *state) viewField(f field) string {
	if s.current == f {
		return " " + s.selectCursor + s.inputs[f].View()
	}
	return "  " + s.inputs[f].View()
}

// item is a saved user.
type item string

func (i item) FilterValue() string { return string(i) }
func (i item) Title() string       { return string(i) }
func (i item) Description() string { return "" }

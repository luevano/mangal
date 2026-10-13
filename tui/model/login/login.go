// Package login is the login screen of the metadata services (AniList, Kitsu,
// MyAnimeList): the logged in user, the saved logins, a new login and the service's API
// client. A Service has the service's part.
package login

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/form"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/util"
)

// noticeDuration is how long the login notices stay, some are long.
const noticeDuration = 5 * time.Second

// Input is a field of a form.
type Input struct {
	Label string
	// Placeholder shows in the empty field
	Placeholder string
	// Secret isn't shown
	Secret bool
	// Required can't be empty
	Required bool
	// Clear empties it once used (a password)
	Clear bool
}

// Service is a service's part of the login screen. The funcs run in the background.
type Service struct {
	// ID is the window's ("login/<ID>")
	ID    string
	Name  string
	Color lipgloss.Color

	// Check refreshes the login on open; an error is logged, the screen stays usable
	Check func(ctx context.Context) error
	// Status reads the login state, no request
	Status func(ctx context.Context) (Status, error)

	// Fields are the new login's form, none for a login in the browser
	Fields []Input
	// FormHint is the subtitle of the new login
	FormHint string
	// FormView is what a new login without fields shows, key is the login key
	FormView func(key string) string
	// LoginLabel is shown while a new login runs
	LoginLabel string
	// LoginNew logs in with the form's values
	LoginNew func(ctx context.Context, values []string) (metadata.User, error)
	// Rejected is the notice of a new login the service rejected (metadata.ErrUnauthorized),
	// the form stays open; empty shows the error
	Rejected string

	LoginSaved func(ctx context.Context, user string) error
	// NotFound is LoginSaved's error for a saved login that's gone
	NotFound error
	// Expired is true for LoginSaved's error of an expired login, the form opens
	Expired func(error) bool

	Logout func() error
	Forget func(user string) error

	// Client is the service's API client, nil without one to set
	Client *Client
}

// Client is the API client form of a service that needs one.
type Client struct {
	Fields []Input
	// Intro is shown above the form
	Intro []string
	// Hint and HintSaved are the subtitles, without and with a saved client
	Hint, HintSaved string
	Load            func() ([]string, error)
	// Save saves the values, the notice says what it did
	Save func(ctx context.Context, values []string) (notice string, err error)
}

// Status is the login state; as a msg it updates the screen.
type Status struct {
	User  metadata.User
	Users []string
	// Values fill the new login's form while it's empty (the saved API client)
	Values []string
	// NoClient opens the API client form
	NoClient bool
}

// ExpiredAs is a Service.Expired for the error type E.
func ExpiredAs[E error](err error) bool {
	var e E
	return errors.As(err, &e)
}

type phase int

const (
	checking phase = iota
	loggedIn
	// saved lists the saved logins
	saved
	// newLogin is a new login
	newLogin
	// client is the API client form
	client
)

// statusMsg is the login state after a check or a login action.
type statusMsg struct {
	Status
	notice string
	// form opens the new login (e.g. the saved one expired)
	form bool
}

// clientMsg opens the API client form with the saved client.
type clientMsg struct{ values []string }

var _ base.State = (*Screen)(nil)

// Screen is a service's login screen.
type Screen struct {
	svc   Service
	phase phase
	user  metadata.User
	users []string
	// configured is true with a saved API client
	configured bool
	// back is the phase esc returns to from the API client form
	back phase

	list   *list.Model
	form   *form.Form
	client *form.Form
	size   base.Size
	keys   keyMap
}

// New is the login screen of svc.
func New(svc Service) *Screen {
	l := list.New(1, 0, "user", "users", []string{}, func(u string) _list.DefaultItem { return item(u) })
	l.SetAccentColor(svc.Color)
	// no filtering: its typing would also be the screen's keys
	l.SetFilteringEnabled(false)
	l.KeyMap.Reverse.SetEnabled(false)
	l.KeyMap.List.GoToStart.SetEnabled(false)
	l.KeyMap.List.GoToEnd.SetEnabled(false)
	l.KeyMap.List.NextPage.SetEnabled(false)
	l.KeyMap.List.PrevPage.SetEnabled(false)

	s := &Screen{svc: svc, list: l, keys: newKeyMap(), configured: svc.Client == nil}
	if len(svc.Fields) != 0 {
		s.form = newForm(svc.Color, "log in", svc.Fields)
	}
	if svc.Client != nil {
		s.client = newForm(svc.Color, "save", svc.Client.Fields)
	}
	s.updateKeybinds()
	return s
}

func newForm(c lipgloss.Color, submit string, inputs []Input) *form.Form {
	fields := make([]*form.Field, len(inputs))
	for i, in := range inputs {
		var validate func(string) error
		if in.Required {
			validate = required
		}
		f := form.Text(in.Label, "", "", validate)
		if in.Secret {
			f = form.Secret(in.Label, "", "", validate)
		}
		f.Input.Placeholder = in.Placeholder
		f.Input.Width = 60
		fields[i] = f
	}
	f := form.New(fields...)
	f.SetAccent(c)
	f.SetSubmit(submit)
	return f
}

func required(v string) error {
	if v == "" {
		return errors.New("required")
	}
	return nil
}

func (s *Screen) Key() string { return "login/" + s.svc.ID }

func (s *Screen) Backable() bool {
	return !s.keys.back.Enabled() && !s.keys.cancel.Enabled()
}

func (s *Screen) Inputting() bool {
	return s.phase == client || s.phase == newLogin && s.form != nil
}

func (s *Screen) Title() base.Title {
	return base.Title{Text: s.svc.Name + " login", Background: s.svc.Color, Foreground: color.Bright}
}

func (s *Screen) Subtitle() string {
	switch {
	case s.phase == client && s.configured:
		return s.svc.Client.HintSaved
	case s.phase == client:
		return s.svc.Client.Hint
	case s.phase == newLogin:
		return s.svc.FormHint
	}
	return ""
}

func (s *Screen) Status() string { return "" }

func (s *Screen) KeyMap() help.KeyMap {
	switch {
	case s.phase == saved:
		return base.CombinedKeyMap(&s.keys, s.list.KeyMap)
	case s.phase == client:
		return util.KeyMap{Short: append(s.client.Keys(), s.keys.cancel)}
	case s.phase == newLogin && s.form != nil:
		return util.KeyMap{Short: append(s.form.Keys(), s.keys.back)}
	}
	return &s.keys
}

func (s *Screen) Resize(size base.Size) tea.Cmd {
	s.size = size
	size.Height = min(size.Height, max(1, len(s.users)))
	return s.list.Resize(size)
}

func (s *Screen) Init(ctx context.Context) tea.Cmd {
	svc := s.svc
	return tea.Batch(textinput.Blink, base.Load(ctx, "Checking the "+svc.Name+" login", func(ctx context.Context) (statusMsg, error) {
		if err := svc.Check(ctx); err != nil {
			// still usable, e.g. to log in again
			log.Warn("%s login: %s", svc.ID, err)
		}
		st, err := svc.Status(ctx)
		return statusMsg{Status: st}, err
	}, toMsg))
}

func toMsg(m statusMsg) tea.Msg { return m }

func (s *Screen) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case Status:
		return s.setStatus(statusMsg{Status: msg})
	case statusMsg:
		return s.setStatus(msg)
	case clientMsg:
		cmd := s.openClient(msg.values)
		s.updateKeybinds()
		return cmd
	case tea.KeyMsg:
		cmd, ok := s.updateKey(ctx, msg)
		s.updateKeybinds()
		if ok {
			return cmd
		}
	}

	switch {
	case s.phase == client:
		return s.submit(ctx, s.client, msg, s.saveClient)
	case s.phase == newLogin && s.form != nil:
		return s.submit(ctx, s.form, msg, s.loginNew)
	case s.phase == saved:
		return s.list.Update(msg)
	}
	return nil
}

// submit passes msg to f, done runs once it's submitted.
func (s *Screen) submit(ctx context.Context, f *form.Form, msg tea.Msg, done func(context.Context) tea.Cmd) tea.Cmd {
	submitted, cmd := f.Update(msg)
	if !submitted {
		return cmd
	}
	if base.Busy(ctx) {
		return cmd
	}
	return tea.Batch(cmd, done(ctx))
}

func (s *Screen) setStatus(msg statusMsg) tea.Cmd {
	s.user, s.users = msg.User, msg.Users
	if s.form != nil && len(msg.Values) != 0 && s.formEmpty() {
		for i, v := range msg.Values[:min(len(msg.Values), len(s.form.Fields))] {
			s.form.Fields[i].Input.SetValue(v)
			s.form.Fields[i].Input.CursorEnd()
		}
	}
	items := make([]_list.Item, len(s.users))
	for i, u := range s.users {
		items[i] = item(u)
	}
	cmd := tea.Batch(s.list.SetItems(items), s.Resize(s.size))

	s.configured = s.svc.Client == nil || !msg.NoClient
	switch {
	case msg.NoClient && s.svc.Client != nil:
		cmd = tea.Batch(cmd, s.openClient(nil))
	case s.user != nil:
		s.phase = loggedIn
	case msg.form || len(s.users) == 0:
		cmd = tea.Batch(cmd, s.openForm())
	default:
		s.phase = saved
	}
	s.updateKeybinds()
	if msg.notice != "" {
		cmd = tea.Batch(cmd, base.NotifyWithDuration(msg.notice, noticeDuration))
	}
	return cmd
}

func (s *Screen) formEmpty() bool {
	for _, f := range s.form.Fields {
		if f.Input.Value() != "" {
			return false
		}
	}
	return true
}

func (s *Screen) openForm() tea.Cmd {
	s.phase = newLogin
	if s.form == nil {
		return nil
	}
	return s.form.Focus(s.form.Current())
}

// openClient shows the API client form with values.
func (s *Screen) openClient(values []string) tea.Cmd {
	if s.phase != client {
		s.back = s.phase
	}
	s.phase = client
	for i, f := range s.client.Fields {
		v := ""
		if i < len(values) {
			v = values[i]
		}
		f.Input.SetValue(v)
		f.Input.CursorEnd()
		f.Err = ""
	}
	return s.client.Focus(0)
}

func (s *Screen) updateKey(ctx context.Context, msg tea.KeyMsg) (tea.Cmd, bool) {
	selected, _ := s.list.SelectedItem().(item)
	switch {
	case key.Matches(msg, s.keys.cancel):
		s.phase = s.back
		if s.phase == newLogin {
			return s.openForm(), true
		}
	case key.Matches(msg, s.keys.back):
		s.phase = saved
	case key.Matches(msg, s.keys.client):
		return s.loadClient(), true
	case key.Matches(msg, s.keys.login):
		if base.Busy(ctx) {
			return nil, true
		}
		if s.phase == newLogin {
			return s.loginNew(ctx), true
		}
		return s.loginSaved(ctx, string(selected)), true
	case key.Matches(msg, s.keys.logout):
		if base.Busy(ctx) {
			return nil, true
		}
		return s.logout(ctx), true
	case key.Matches(msg, s.keys.new):
		return s.openForm(), true
	case key.Matches(msg, s.keys.forget):
		if base.Busy(ctx) {
			return nil, true
		}
		return s.forget(ctx, string(selected)), true
	default:
		return nil, false
	}
	return nil, true
}

// loadClient reads the saved API client, no request.
func (s *Screen) loadClient() tea.Cmd {
	load := s.svc.Client.Load
	return func() tea.Msg {
		values, err := load()
		if err != nil {
			return err
		}
		return clientMsg{values}
	}
}

func (s *Screen) saveClient(ctx context.Context) tea.Cmd {
	values := formValues(s.client)
	save, check, status := s.svc.Client.Save, s.svc.Check, s.svc.Status
	id := s.svc.ID
	return base.Load(ctx, "", func(ctx context.Context) (statusMsg, error) {
		notice, err := save(ctx, values)
		if err != nil {
			return statusMsg{}, err
		}
		if err := check(ctx); err != nil {
			log.Warn("%s login: %s", id, err)
		}
		st, err := status(ctx)
		return statusMsg{Status: st, notice: notice}, err
	}, toMsg)
}

func formValues(f *form.Form) []string {
	values := make([]string, len(f.Fields))
	for i, fl := range f.Fields {
		values[i] = fl.Input.Value()
	}
	return values
}

func (s *Screen) loginSaved(ctx context.Context, user string) tea.Cmd {
	if user == "" {
		return nil
	}
	svc := s.svc
	return base.Load(ctx, fmt.Sprintf("Logging in %q", user), func(ctx context.Context) (statusMsg, error) {
		err := svc.LoginSaved(ctx, user)
		msg := statusMsg{notice: fmt.Sprintf("Logged in as %q", user)}
		switch {
		case svc.NotFound != nil && errors.Is(err, svc.NotFound):
			msg.notice = fmt.Sprintf("The login of %q is gone, removed it", user)
		case err != nil && svc.Expired != nil && svc.Expired(err):
			msg.notice, msg.form = err.Error(), true
		case err != nil:
			return statusMsg{}, err
		}
		msg.Status, err = svc.Status(ctx)
		return msg, err
	}, toMsg)
}

func (s *Screen) loginNew(ctx context.Context) tea.Cmd {
	var values []string
	if s.form != nil {
		values = formValues(s.form)
		for i, in := range s.svc.Fields {
			if in.Clear {
				s.form.Fields[i].Input.SetValue("")
			}
		}
	}
	svc := s.svc
	return base.Load(ctx, svc.LoginLabel, func(ctx context.Context) (statusMsg, error) {
		user, err := svc.LoginNew(ctx, values)
		if svc.Rejected != "" && errors.Is(err, metadata.ErrUnauthorized) {
			st, err := svc.Status(ctx)
			return statusMsg{Status: st, notice: svc.Rejected, form: true}, err
		}
		if err != nil {
			return statusMsg{}, fmt.Errorf("%s login: %w", svc.ID, err)
		}
		st, err := svc.Status(ctx)
		return statusMsg{Status: st, notice: fmt.Sprintf("Logged in as %q", user.Name())}, err
	}, toMsg)
}

func (s *Screen) logout(ctx context.Context) tea.Cmd {
	svc := s.svc
	return base.Load(ctx, "", func(ctx context.Context) (statusMsg, error) {
		if err := svc.Logout(); err != nil {
			return statusMsg{}, fmt.Errorf("%s logout: %w", svc.ID, err)
		}
		st, err := svc.Status(ctx)
		return statusMsg{Status: st, notice: "Logged out until the next login"}, err
	}, toMsg)
}

func (s *Screen) forget(ctx context.Context, user string) tea.Cmd {
	if user == "" {
		return nil
	}
	svc := s.svc
	return base.Load(ctx, "", func(ctx context.Context) (statusMsg, error) {
		if err := svc.Forget(user); err != nil {
			return statusMsg{}, err
		}
		st, err := svc.Status(ctx)
		return statusMsg{Status: st, notice: fmt.Sprintf("Forgot the login of %q", user)}, err
	}, toMsg)
}

// updateKeybinds enables the keys of the phase.
func (s *Screen) updateKeybinds() {
	inForm := s.phase == newLogin
	hasClient := s.svc.Client != nil

	s.keys.cancel.SetEnabled(s.phase == client && s.configured)
	s.keys.back.SetEnabled(inForm && len(s.users) != 0)
	// a form's enter logs in
	s.keys.login.SetEnabled(s.phase == saved || inForm && s.form == nil)
	s.keys.logout.SetEnabled(s.phase == loggedIn)
	s.keys.new.SetEnabled(s.phase == saved)
	s.keys.forget.SetEnabled(s.phase == saved && len(s.users) != 0)
	s.keys.client.SetEnabled(hasClient && (s.phase == loggedIn || s.phase == saved || inForm && s.form == nil))
}

func (s *Screen) View() string {
	switch s.phase {
	case checking:
		return ""
	case client:
		lines := append([]string{}, s.svc.Client.Intro...)
		if len(lines) != 0 {
			lines = append(lines, " ")
		}
		return lipgloss.JoinVertical(lipgloss.Left, append(lines, s.client.View())...)
	case loggedIn:
		return "Logged in as " + s.user.String()
	case saved:
		return lipgloss.JoinVertical(lipgloss.Left, "Saved logins", " ", s.list.View())
	}
	if s.form == nil {
		return s.svc.FormView(s.keys.login.Help().Key)
	}
	return s.form.View()
}

// item is a saved user.
type item string

func (i item) FilterValue() string { return string(i) }
func (i item) Title() string       { return string(i) }
func (i item) Description() string { return "" }

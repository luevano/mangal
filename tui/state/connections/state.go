// Package connections is the TUI's picker of what it's connected to: this machine or a mangal
// serve. Switching ends the screens of one backend and starts at home on the other.
package connections

import (
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	_list "charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/remote"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/confirm"
	"github.com/luevano/mangal/v5/tui/model/huhform"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/util"
)

// item is a row: this machine, or a named server.
type item struct {
	name, url string
	current   bool
	token     bool
}

func (i item) FilterValue() string { return i.name }

func (i item) Title() string {
	if i.current {
		return i.name + "  (in use)"
	}
	return i.name
}

func (i item) Description() string {
	switch {
	case i.name == config.LocalConnection:
		return "this machine"
	case i.token:
		return i.url + ", token saved"
	}
	return i.url + ", no token"
}

// items are this machine, then the servers by name, with the one in use marked.
func items(current string) []item {
	rows := []item{{name: config.LocalConnection, current: current == ""}}
	conns := config.Connections.Get()
	for _, name := range slices.Sorted(maps.Keys(conns)) {
		url, err := remote.NormalizeURL(conns[name])
		if err != nil {
			url = conns[name]
		}
		_, from, err := remote.Token(url)
		if err != nil {
			log.Warn("tui: the token of %s: %s", name, err)
		}
		rows = append(rows, item{name: name, url: url, current: url == current, token: from != ""})
	}
	return rows
}

type state struct {
	list.Screen
	// current is the server in use, "" for this machine; why is why the TUI opened here
	current, why string
	// form is what's being filled: a new connection, or a login
	form  *huhform.Form
	login string
	err   string

	add, remove, logIn, choose key.Binding
}

// New is the picker, why says what's wrong with the connection when the TUI opens at it.
func New(current, why string) base.State {
	s := &state{
		current: current, why: why,
		add: base.KeyAdd, remove: base.KeyDelete, logIn: base.KeyLogIn,
		choose: util.Bind("switch", "enter"),
	}
	s.Screen = list.NewScreen(list.New(2, 1, "connection", "connections", items(current), func(i item) _list.DefaultItem { return i }), s.keyMap)
	s.Confirm = confirm.NewDialog(40, color.Warning)
	return s
}

func (s *state) keyMap() help.KeyMap {
	if s.form != nil {
		return util.KeyMap{Short: s.form.Keys()}
	}
	keys := []key.Binding{s.choose, s.add, s.remove, s.logIn}
	return util.KeyMap{Short: keys, Full: [][]key.Binding{keys}}
}

func (s *state) Key() string { return "connections" }

func (s *state) Title() base.Title { return base.Title{Text: "Connections"} }

func (s *state) Subtitle() string {
	if s.why != "" {
		return "not connected"
	}
	if s.current == "" {
		return "on this machine"
	}
	return "on " + remote.Name(s.current)
}

func (s *state) Backable() bool { return s.form == nil && s.Screen.Backable() }

func (s *state) Inputting() bool { return s.form != nil || s.Screen.Inputting() }

func (s *state) reload() tea.Cmd {
	rows := items(s.current)
	listItems := make([]_list.Item, len(rows))
	for n, r := range rows {
		listItems[n] = r
	}
	return s.List.SetItemsKeep(listItems)
}

// savedMsg says the connection list changed.
type savedMsg struct{ notice string }

// loggedInMsg is a token checked and saved.
type loggedInMsg struct{ notice string }

type removeMsg struct{ name string }

func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	if cmd, ok := s.Answer(msg); ok {
		return cmd
	}
	switch msg := msg.(type) {
	case savedMsg:
		return tea.Batch(s.reload(), base.Notify(msg.notice))
	case loggedInMsg:
		s.form = nil
		return tea.Batch(s.reload(), base.Notify(msg.notice))
	case removeMsg:
		if err := remote.RemoveConnection(msg.name); err != nil {
			return func() tea.Msg { return err }
		}
		return tea.Batch(s.reload(), base.Notify("Removed "+msg.name+", its saved token stays until a log out"))
	case loginErrMsg:
		s.err = msg.err.Error()
		return nil
	}
	if s.form != nil {
		return s.updateForm(ctx, msg)
	}
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok || s.List.Filtering() {
		return s.List.Update(msg)
	}
	i, selected := s.List.SelectedItem().(item)
	switch {
	case key.Matches(keyMsg, s.add):
		s.openAdd()
		return s.form.Focus(0)
	case !selected:
	case key.Matches(keyMsg, s.choose):
		if i.current && s.why == "" {
			return base.Notify("Already on " + i.name)
		}
		return base.SwitchTo(i.name)
	case key.Matches(keyMsg, s.remove):
		switch {
		case i.url == "":
			return base.NotifyWarning("This machine can't be removed")
		case i.current:
			return base.NotifyWarning(i.name + " is in use: switch to another first")
		}
		name := i.name
		s.Confirm.AskDelete("Remove", fmt.Sprintf("Remove the connection %s?", name), s.View(), func() tea.Msg { return removeMsg{name: name} })
		return nil
	case key.Matches(keyMsg, s.logIn):
		if i.url == "" {
			return base.NotifyWarning("This machine has no login")
		}
		s.err, s.login = "", i.url
		host, _ := os.Hostname()
		s.form = huhform.New(huhform.Text("User", "", "empty to paste an API token (from mangal token add on "+i.name+")", nil),
			huhform.Secret("Password or token", "", "the password isn't kept, the server answers with this device's token", nil),
			huhform.Text("Device", host, "this device's name there, its token is tui@<device> (with a user)", func(v string) error {
				if v == "" {
					return nil
				}
				return core.CheckDeviceName(v)
			}))
		s.form.SetSubmit("log in")
		return s.form.Focus(0)
	}
	return s.List.Update(msg)
}

func (s *state) openAdd() {
	s.err, s.login = "", ""
	name := huhform.Text("Name", "", "lowercase, e.g. nas", config.ValidConnectionName)
	address := huhform.Text("Address", "", "https://mangal.example.com, http://nas.lan:6977", func(v string) error {
		_, err := remote.NormalizeURL(v)
		return err
	})
	s.form = huhform.New(name, address)
	s.form.SetSubmit("add")
}

func (s *state) updateForm(ctx context.Context, msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok && k.String() == "esc" {
		s.form, s.err = nil, ""
		return nil
	}
	submitted, cmd := s.form.Update(msg)
	if !submitted {
		return cmd
	}
	if s.login != "" {
		return s.saveToken(ctx)
	}
	name, address := s.form.Fields[0].Value(), s.form.Fields[1].Value()
	if err := remote.AddConnection(name, address); err != nil {
		s.err = err.Error()
		return nil
	}
	s.form, s.err = nil, ""
	return tea.Batch(s.reload(), base.Notify("Added "+name))
}

// loginErrMsg is why a login wasn't saved, shown under the form.
type loginErrMsg struct{ err error }

func (s *state) saveToken(ctx context.Context) tea.Cmd {
	url, user, secret, device := s.login, s.form.Fields[0].Value(), s.form.Fields[1].Raw(), s.form.Fields[2].Value()
	warn := func(w string) { log.Warn("%s", w) }
	if user == "" {
		secret = strings.TrimSpace(secret)
		if secret == "" {
			s.err = "no token: nothing saved"
			return nil
		}
		return base.Load(ctx, "Checking the token on "+remote.Name(url), func(ctx context.Context) (tea.Msg, error) {
			who, err := remote.SaveToken(ctx, url, secret, warn)
			if err != nil {
				return loginErrMsg{err: err}, nil
			}
			return loggedInMsg{notice: "Saved the token " + who + " for " + remote.Name(url)}, nil
		}, func(m tea.Msg) tea.Msg { return m })
	}
	if err := remote.CheckPasswordAddress(url); err != nil {
		s.err = err.Error()
		return nil
	}
	if secret == "" {
		s.err = "no password: nothing saved"
		return nil
	}
	return base.Load(ctx, "Logging in to "+remote.Name(url), func(ctx context.Context) (tea.Msg, error) {
		who, err := remote.Login(ctx, url, user, secret, device, warn)
		if err != nil {
			return loginErrMsg{err: err}, nil
		}
		return loggedInMsg{notice: "Logged in as " + user + ", saved this device's token " + who + " for " + remote.Name(url)}, nil
	}, func(m tea.Msg) tea.Msg { return m })
}

func (s *state) View() string {
	if s.form != nil {
		lines := []string{s.form.View()}
		if s.err != "" {
			lines = append(lines, "", style.Normal.Error.Render(s.err))
		}
		return s.Host.View(func() string { return lipgloss.JoinVertical(lipgloss.Left, lines...) })
	}
	if s.why == "" {
		return s.Screen.View()
	}
	return lipgloss.JoinVertical(lipgloss.Left, style.Normal.Error.Render(s.why), "", s.Screen.View())
}

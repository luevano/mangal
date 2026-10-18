package accounts

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/huhform"
	"github.com/luevano/mangal/v5/tui/util"
)

// The users who log into mangal serve and its API tokens are rows after the accounts:
// n adds one (a token on the tokens' rows), e changes a user's permissions, i its
// password, o ends its logins, d removes (pressed twice).

type rowKind int

const (
	rowAccount rowKind = iota
	rowUser
	rowNoUsers
	rowToken
	rowNoTokens
)

type rowRef struct {
	kind rowKind
	i    int
}

// userForm is what the open form does.
type userForm int

const (
	formNone userForm = iota
	formAddUser
	formAddToken
	formPassword
	formPerms
)

type usersMsg struct {
	users  []core.User
	tokens []core.Token
	err    error
}

// tokenMadeMsg is a token made, its secret shown once.
type tokenMadeMsg struct {
	token  core.Token
	secret string
}

// refs are the rows: the accounts, the users, the tokens (a placeholder when none).
func (s *state) refs() []rowRef {
	refs := make([]rowRef, 0, len(s.entries)+len(s.users)+len(s.tokens)+2)
	for i := range s.entries {
		refs = append(refs, rowRef{rowAccount, i})
	}
	if !s.usersLoaded {
		return refs
	}
	for i := range s.users {
		refs = append(refs, rowRef{rowUser, i})
	}
	if len(s.users) == 0 {
		refs = append(refs, rowRef{rowNoUsers, 0})
	}
	for i := range s.tokens {
		refs = append(refs, rowRef{rowToken, i})
	}
	if len(s.tokens) == 0 {
		refs = append(refs, rowRef{rowNoTokens, 0})
	}
	return refs
}

func (s *state) selectedRef() (rowRef, bool) {
	refs := s.refs()
	i := s.table.Cursor()
	if i < 0 || i >= len(refs) {
		return rowRef{}, false
	}
	return refs[i], true
}

func (s *state) setRows() {
	var rows []table.Row
	for _, r := range s.refs() {
		switch r.kind {
		case rowAccount:
			rows = append(rows, accountRow(s.entries[r.i]))
		case rowUser:
			u := s.users[r.i]
			last := "never logged in"
			if !u.LastLogin.IsZero() {
				last = "last login " + u.LastLogin.Format(time.DateTime)
			}
			rows = append(rows, table.Row{"mangal user", u.Name, strings.Join(u.Perms, ","), "", last, ""})
		case rowNoUsers:
			notes := "none: " + s.keys.add.Help().Key + " adds one (the web UI's login)"
			if s.usersErr != "" {
				notes = s.usersErr
			}
			rows = append(rows, table.Row{"mangal users", "", "", "", notes, ""})
		case rowToken:
			t := s.tokens[r.i]
			rows = append(rows, table.Row{"API token", t.Name, strings.Join(t.Scopes, ","), "", "made " + t.Created.Format(time.DateOnly), ""})
		case rowNoTokens:
			rows = append(rows, table.Row{"API tokens", "", "", "", "none: " + s.keys.add.Help().Key + " here makes one (for programs)", ""})
		}
	}
	s.table.SetRows(rows)
	s.table.SetHeight(len(rows) + 1)
}

func (s *state) usersService() core.Users {
	if s.usersSvc != nil {
		return s.usersSvc
	}
	return core.NewUsers()
}

func (s *state) loadUsers(ctx context.Context) tea.Cmd {
	svc := s.usersService()
	return base.Load(ctx, "Loading the users", func(ctx context.Context) (usersMsg, error) {
		users, err := svc.List(ctx)
		if err != nil {
			return usersMsg{err: err}, nil
		}
		tokens, err := svc.Tokens(ctx)
		return usersMsg{users: users, tokens: tokens, err: err}, nil
	}, func(m usersMsg) tea.Msg { return m })
}

// userAction runs an action on the users and loads them again.
func (s *state) userAction(ctx context.Context, label string, work func(context.Context, core.Users) (tea.Msg, error)) tea.Cmd {
	svc := s.usersService()
	return base.Load(ctx, label, func(ctx context.Context) (tea.Msg, error) {
		return work(ctx, svc)
	}, func(m tea.Msg) tea.Msg { return m })
}

func done(message string) tea.Msg {
	return usersChangedMsg{notice: message}
}

// usersChangedMsg reloads the users and says what changed.
type usersChangedMsg struct{ notice string }

func validScopes(v string) error {
	if v == "" {
		return nil
	}
	_, err := core.NormalizeScopes(strings.Split(v, ","))
	return err
}

func validPassword(v string) error {
	if len(v) < core.MinPasswordLen {
		return fmt.Errorf("%d characters or more", core.MinPasswordLen)
	}
	return nil
}

func required(v string) error {
	if v == "" {
		return fmt.Errorf("required")
	}
	return nil
}

func (s *state) openForm(kind userForm, fields ...*huhform.Field) tea.Cmd {
	for _, f := range fields {
		f.SetWidth(50)
	}
	s.form, s.formKind, s.inputting = huhform.New(fields...), kind, true
	return s.form.Focus(0)
}

// updateUsersKeys handles the users' keys on their rows; false when the key isn't theirs.
func (s *state) updateUsersKeys(ctx context.Context, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	ref, ok := s.selectedRef()
	if !ok {
		return nil, false
	}
	confirmed := s.confirm != "" && key.Matches(msg, s.keys.remove)
	pending := s.confirm
	s.confirm = ""
	switch ref.kind {
	case rowUser:
		u := s.users[ref.i]
		switch {
		case key.Matches(msg, s.keys.add):
			return s.addUserForm(), true
		case key.Matches(msg, s.keys.edit):
			s.target = u.Name
			return s.openForm(formPerms, huhform.Text("Permissions", strings.Join(u.Perms, ","), "read, progress, watch, write", func(v string) error {
				if err := required(v); err != nil {
					return err
				}
				return validScopes(v)
			})), true
		case key.Matches(msg, s.keys.login):
			s.target = u.Name
			return s.openForm(formPassword, huhform.Secret("New password", "", "its logins end", validPassword)), true
		case key.Matches(msg, s.keys.logout):
			return s.userAction(ctx, "Ending the logins", func(ctx context.Context, svc core.Users) (tea.Msg, error) {
				return done("Ended the logins of " + u.Name), svc.EndLogins(ctx, u.Name)
			}), true
		case key.Matches(msg, s.keys.remove):
			if !confirmed || pending != "user/"+u.Name {
				s.confirm = "user/" + u.Name
				return base.Notify(fmt.Sprintf("Press %s again to remove the user %s", s.keys.remove.Help().Key, u.Name)), true
			}
			return s.userAction(ctx, "Removing the user", func(ctx context.Context, svc core.Users) (tea.Msg, error) {
				return done("Removed the user " + u.Name), svc.Remove(ctx, u.Name)
			}), true
		}
	case rowNoUsers:
		if key.Matches(msg, s.keys.add) {
			return s.addUserForm(), true
		}
	case rowToken:
		t := s.tokens[ref.i]
		switch {
		case key.Matches(msg, s.keys.add):
			return s.addTokenForm(), true
		case key.Matches(msg, s.keys.edit):
			return base.Notify("A token's scopes don't change: make another, remove this one"), true
		case key.Matches(msg, s.keys.remove):
			if !confirmed || pending != "token/"+t.Name {
				s.confirm = "token/" + t.Name
				return base.Notify(fmt.Sprintf("Press %s again to remove the API token %s", s.keys.remove.Help().Key, t.Name)), true
			}
			return s.userAction(ctx, "Removing the token", func(ctx context.Context, svc core.Users) (tea.Msg, error) {
				return done("Removed the API token " + t.Name), svc.RemoveToken(ctx, t.Name)
			}), true
		}
	case rowNoTokens:
		if key.Matches(msg, s.keys.add) {
			return s.addTokenForm(), true
		}
	}
	return nil, false
}

func (s *state) addUserForm() tea.Cmd {
	hint := "read, progress, watch, write; empty: read,progress"
	if len(s.users) == 0 {
		hint = "read, progress, watch, write; empty: every one (the first user)"
	}
	return s.openForm(formAddUser,
		huhform.Text("Name", "", "", func(v string) error {
			if err := required(v); err != nil {
				return err
			}
			return core.CheckUserName(v)
		}),
		huhform.Secret("Password", "", "", validPassword),
		huhform.Text("Permissions", "", hint, validScopes),
	)
}

func (s *state) addTokenForm() tea.Cmd {
	return s.openForm(formAddToken,
		huhform.Text("Name", "", "what it's for: a script, an app", required),
		huhform.Text("Scopes", "read", "read, progress, watch, write", validScopes),
	)
}

func scopesOf(v string) []string {
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}

// submitForm runs the form's action.
func (s *state) submitForm(ctx context.Context) tea.Cmd {
	f, kind, target := s.form, s.formKind, s.target
	s.formKind, s.inputting = formNone, false
	switch kind {
	case formAddUser:
		name, password, perms := f.Fields[0].Value(), f.Fields[1].Value(), scopesOf(f.Fields[2].Value())
		return s.userAction(ctx, "Adding the user", func(ctx context.Context, svc core.Users) (tea.Msg, error) {
			u, err := svc.Add(ctx, name, password, perms)
			return done(fmt.Sprintf("Added the user %s (%s)", u.Name, strings.Join(u.Perms, ", "))), err
		})
	case formAddToken:
		name, scopes := f.Fields[0].Value(), scopesOf(f.Fields[1].Value())
		return s.userAction(ctx, "Making the token", func(ctx context.Context, svc core.Users) (tea.Msg, error) {
			t, secret, err := svc.AddToken(ctx, name, scopes)
			return tokenMadeMsg{token: t, secret: secret}, err
		})
	case formPassword:
		password := f.Fields[0].Value()
		return s.userAction(ctx, "Changing the password", func(ctx context.Context, svc core.Users) (tea.Msg, error) {
			return done("Changed the password of " + target + ", its logins ended"), svc.SetPassword(ctx, target, password)
		})
	case formPerms:
		perms := scopesOf(f.Fields[0].Value())
		return s.userAction(ctx, "Changing the permissions", func(ctx context.Context, svc core.Users) (tea.Msg, error) {
			return done("Changed the permissions of " + target + ", its logins ended"), svc.SetPerms(ctx, target, perms)
		})
	}
	return nil
}

func (s *state) updateForm(ctx context.Context, msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok && key.Matches(k, s.keys.cancel) {
		s.formKind, s.inputting = formNone, false
		return nil
	}
	submitted, cmd := s.form.Update(msg)
	if submitted {
		return s.submitForm(ctx)
	}
	return cmd
}

// updateUsers handles the users' messages, false when msg isn't one.
func (s *state) updateUsers(ctx context.Context, msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case usersMsg:
		s.usersLoaded = true
		s.users, s.tokens, s.usersErr = msg.users, msg.tokens, ""
		if msg.err != nil {
			s.usersErr = msg.err.Error()
		}
		s.setRows()
		return nil, true
	case usersChangedMsg:
		return tea.Batch(base.Notify(msg.notice), s.loadUsers(ctx)), true
	case tokenMadeMsg:
		notice := util.CopyNotice(msg.secret, fmt.Sprintf("API token %s: %s (copied; it isn't shown again)", msg.token.Name, msg.secret))
		return tea.Batch(func() tea.Msg {
			return base.NotificationWithDurationMsg{NotificationMsg: base.NotificationMsg{Message: notice}, Duration: time.Minute}
		}, s.loadUsers(ctx)), true
	}
	return nil, false
}

// usersKeys are the keys of the selected row, for the help.
func (s *state) usersKeys() []key.Binding {
	ref, ok := s.selectedRef()
	if !ok {
		return nil
	}
	switch ref.kind {
	case rowUser:
		return []key.Binding{s.keys.add, s.keys.edit, s.keys.login, s.keys.logout, s.keys.remove}
	case rowToken:
		return []key.Binding{s.keys.add, s.keys.remove}
	case rowNoUsers, rowNoTokens:
		return []key.Binding{s.keys.add}
	}
	return nil
}

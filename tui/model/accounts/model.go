// Package accounts shows the accounts mangal can use: the Anilist, Kitsu and MyAnimeList
// logins, the providers' accounts (e.g. the MangaPlus subscription) and the Discord webhook.
package accounts

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	mconfig "github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/anilist"
	"github.com/luevano/mangal/v5/tui/model/kitsu"
	"github.com/luevano/mangal/v5/tui/model/myanimelist"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/luevano/mangal/v5/util/cache"
)

const mangaPlusID = "mango-mangaplus"

var _ base.State = (*state)(nil)

type state struct {
	refresh bool
	entries []client.AccountEntry
	loaded  bool
	table   table.Model
	secret  textinput.Model
	keys    keyMap

	inputting bool
	// discord is true when the input is the Discord webhook, else the MangaPlus secret
	discord bool
}

type loadedMsg []client.AccountEntry

// New is the accounts screen, refresh fetches them the first time instead of using the
// cached ones.
func New(refresh bool) base.State {
	t := table.New(
		table.WithColumns([]table.Column{
			{Title: "Service", Width: 13},
			{Title: "User", Width: 16},
			{Title: "Status", Width: 14},
			{Title: "Renews/expires", Width: 15},
			{Title: "Notes", Width: 38},
			{Title: "Checked", Width: 10},
		}),
		table.WithFocused(true),
		table.WithStyles(table.Styles{
			Header:   style.Bold.Accent,
			Cell:     style.Normal.Base,
			Selected: style.Normal.Background.Background(color.Accent),
		}),
	)
	secret := textinput.New()
	secret.Prompt = "MangaPlus secret: "
	secret.EchoMode = textinput.EchoPassword
	secret.Width = 50

	return &state{
		refresh: refresh,
		table:   t,
		secret:  secret,
		keys:    newKeyMap(),
	}
}

func (s *state) Key() string     { return "accounts" }
func (s *state) Backable() bool  { return !s.inputting }
func (s *state) Inputting() bool { return s.inputting }
func (s *state) Title() base.Title {
	return base.Title{Text: "Accounts"}
}
func (s *state) Status() string                { return "" }
func (s *state) Resize(size base.Size) tea.Cmd { return nil }

func (s *state) Subtitle() string {
	if s.inputting && s.discord {
		return "the webhook url of the notifications (empty removes it)"
	}
	if s.inputting {
		return "the device secret of the official app (empty removes it), see the mangoplus docs"
	}
	return "cached for 12 hours, " + s.keys.refresh.Help().Key + " fetches them now"
}

func (s *state) KeyMap() help.KeyMap {
	if s.inputting {
		return util.KeyMap{Short: []key.Binding{s.keys.save, s.keys.cancel}}
	}
	return util.KeyMap{Short: []key.Binding{s.keys.refresh, s.keys.login, s.keys.copy}}
}

func (s *state) Init(ctx context.Context) tea.Cmd {
	return s.load(ctx, s.refresh)
}

func (s *state) load(ctx context.Context, refresh bool) tea.Cmd {
	label := "Loading the accounts"
	if refresh {
		label = "Fetching the accounts"
	}
	return base.Load(ctx, label, func(ctx context.Context) ([]client.AccountEntry, error) {
		return client.Accounts(ctx, refresh)
	}, func(entries []client.AccountEntry) tea.Msg { return loadedMsg(entries) })
}

func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case loadedMsg:
		s.loaded = true
		s.entries = msg
		s.table.SetRows(rows(msg))
		s.table.SetHeight(len(msg) + 1)
		return nil
	case base.RestoredMsg:
		// e.g. after a login
		return s.load(ctx, false)
	case tea.KeyMsg:
		if s.inputting {
			return s.updateSecret(ctx, msg)
		}
		switch {
		case key.Matches(msg, s.keys.refresh):
			if base.Busy(ctx) {
				return nil
			}
			return s.load(ctx, true)
		case key.Matches(msg, s.keys.login):
			return s.login()
		case key.Matches(msg, s.keys.copy):
			entry, ok := s.selected()
			if !ok || entry.URL == "" {
				return nil
			}
			return copyCmd(entry.URL)
		}
	}
	if s.inputting {
		var cmd tea.Cmd
		s.secret, cmd = s.secret.Update(msg)
		return cmd
	}
	var cmd tea.Cmd
	s.table, cmd = s.table.Update(msg)
	return cmd
}

func copyCmd(url string) tea.Cmd {
	return func() tea.Msg {
		if err := clipboard.WriteAll(url); err != nil {
			return base.NotificationMsg{Message: "Copy: " + err.Error()}
		}
		return base.NotificationMsg{Message: "Copied " + url}
	}
}

func (s *state) selected() (client.AccountEntry, bool) {
	i := s.table.Cursor()
	if i < 0 || i >= len(s.entries) {
		return client.AccountEntry{}, false
	}
	return s.entries[i], true
}

func (s *state) login() tea.Cmd {
	entry, ok := s.selected()
	if !ok {
		return nil
	}
	switch {
	case entry.Metadata == metadata.IDSourceAnilist:
		return func() tea.Msg { return anilist.New() }
	case entry.Metadata == metadata.IDSourceKitsu:
		return func() tea.Msg { return kitsu.New() }
	case entry.Metadata == metadata.IDSourceMyAnimeList:
		return func() tea.Msg { return myanimelist.New() }
	case entry.Provider == mangaPlusID:
		s.inputting, s.discord = true, false
		s.secret.Prompt = "MangaPlus secret: "
		s.secret.SetValue("")
		return s.secret.Focus()
	case entry.Discord:
		s.inputting, s.discord = true, true
		s.secret.Prompt = "Discord webhook: "
		s.secret.SetValue("")
		return s.secret.Focus()
	}
	return base.Notify("Nothing to log into for " + entry.Service)
}

func (s *state) updateSecret(ctx context.Context, msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, s.keys.cancel):
		s.inputting = false
		s.secret.Blur()
		return nil
	case key.Matches(msg, s.keys.save):
		s.inputting = false
		s.secret.Blur()
		secret := strings.TrimSpace(s.secret.Value())
		if s.discord {
			return s.saveWebhook(ctx, secret)
		}
		// the keyring can take a while (D-Bus)
		return func() tea.Msg {
			if err := cache.SetMangaPlusSecret(secret); err != nil {
				return err
			}
			// the loaded providers keep the secret they started with
			return base.NotificationWithDurationMsg{
				NotificationMsg: base.NotificationMsg{Message: "Saved, used from the next run of mangal"},
				Duration:        3 * time.Second,
			}
		}
	}
	var cmd tea.Cmd
	s.secret, cmd = s.secret.Update(msg)
	return cmd
}

// saveWebhook saves the Discord webhook and reloads the rows.
func (s *state) saveWebhook(ctx context.Context, url string) tea.Cmd {
	if err := mconfig.ValidateWebhookURL(url); err != nil {
		return base.Notify(err.Error())
	}
	entries := slices.Clone(s.entries)
	return base.Load(ctx, "Saving the webhook", func(context.Context) ([]client.AccountEntry, error) {
		if err := cache.SetDiscordWebhook(url); err != nil {
			return nil, err
		}
		for i, e := range entries {
			if e.Discord {
				entries[i] = client.DiscordAccount()
			}
		}
		return entries, nil
	}, func(entries []client.AccountEntry) tea.Msg { return loadedMsg(entries) })
}

func (s *state) View() string {
	if !s.loaded {
		return ""
	}
	if s.inputting {
		return lipgloss.JoinVertical(lipgloss.Left, s.table.View(), " ", s.secret.View())
	}
	return s.table.View()
}

func rows(entries []client.AccountEntry) []table.Row {
	rows := make([]table.Row, len(entries))
	for i, e := range entries {
		expires := ""
		if !e.Expires.IsZero() {
			expires = e.Expires.Format(time.DateOnly)
		}
		notes := strings.Join(e.Details, ", ")
		if e.Err != nil {
			notes = e.Err.Error()
		}
		rows[i] = table.Row{e.Service, e.User, e.Status, expires, notes, ago(e.Checked)}
	}
	return rows
}

// ago is how long ago, roughly.
func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}

type keyMap struct {
	refresh,
	login,
	copy,
	save,
	cancel key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		refresh: base.KeyRefresh,
		login:   base.KeyAccountLogIn,
		copy:    base.KeyCopyURL,
		save:    util.Bind("save", "enter"),
		cancel:  util.Bind("cancel", "esc"),
	}
}

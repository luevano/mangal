// Package accounts shows the accounts mangal uses: the Anilist login and the providers'
// accounts (e.g. the MangaPlus subscription).
package accounts

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/style"
	mhelp "github.com/luevano/mangal/v5/tui/model/help"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/luevano/mangal/v5/util/cache"
)

const mangaPlusID = "mango-mangaplus"

var _ tea.Model = (*Model)(nil)

// Model implements tea.Model.
type Model struct {
	ctx     context.Context
	entries []client.AccountEntry
	table   table.Model
	secret  textinput.Model
	help    help.Model
	keyMap  KeyMap

	loading, inputting bool
	notification       string

	// LoginAnilist is set when the user asked to log into Anilist, done after quitting.
	LoginAnilist bool

	standalone, quitting bool
}

type loadedMsg struct {
	entries []client.AccountEntry
	err     error
}

type notificationMsg string

// LoginAnilistMsg asks the TUI to open the Anilist login (when not standalone).
type LoginAnilistMsg struct{}

// New creates the model, standalone for mangal accounts (with its own quit keys).
func New(ctx context.Context, standalone bool) *Model {
	t := table.New(
		table.WithColumns([]table.Column{
			{Title: "Service", Width: 12},
			{Title: "User", Width: 16},
			{Title: "Status", Width: 14},
			{Title: "Renews/expires", Width: 15},
			{Title: "Notes", Width: 30},
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

	m := &Model{
		ctx:        ctx,
		table:      t,
		secret:     secret,
		help:       mhelp.New(),
		keyMap:     newKeyMap(),
		standalone: standalone,
		loading:    true,
	}
	if !standalone {
		m.keyMap.quit.SetEnabled(false)
	}
	return m
}

func (m *Model) load(refresh bool) tea.Cmd {
	m.loading = true
	return func() tea.Msg {
		entries, err := client.Accounts(m.ctx, refresh)
		return loadedMsg{entries: entries, err: err}
	}
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return m.load(false)
}

// SetContext replaces the context for the requests, the TUI's changes when going back.
func (m *Model) SetContext(ctx context.Context) {
	m.ctx = ctx
}

// Reload lists the accounts again (e.g. after logging in), from the cache.
func (m *Model) Reload() tea.Cmd {
	m.notification = ""
	return m.load(false)
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case loadedMsg:
		m.loading = false
		if msg.err != nil {
			m.notification = msg.err.Error()
			return m, nil
		}
		m.entries = msg.entries
		m.table.SetRows(rows(msg.entries))
		m.table.SetHeight(len(msg.entries) + 1)
		return m, nil
	case notificationMsg:
		m.notification = string(msg)
		return m, nil
	case tea.KeyMsg:
		if m.inputting {
			return m, m.updateSecret(msg)
		}
		switch {
		case key.Matches(msg, m.keyMap.quit):
			m.quitting = true
			return m, tea.Quit
		case key.Matches(msg, m.keyMap.refresh):
			m.notification = ""
			return m, m.load(true)
		case key.Matches(msg, m.keyMap.login):
			return m, m.login()
		case key.Matches(msg, m.keyMap.copy):
			if entry, ok := m.selected(); ok && entry.URL != "" {
				if err := clipboard.WriteAll(entry.URL); err != nil {
					m.notification = err.Error()
				} else {
					m.notification = "copied " + entry.URL
				}
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *Model) selected() (client.AccountEntry, bool) {
	i := m.table.Cursor()
	if i < 0 || i >= len(m.entries) {
		return client.AccountEntry{}, false
	}
	return m.entries[i], true
}

func (m *Model) login() tea.Cmd {
	entry, ok := m.selected()
	if !ok {
		return nil
	}
	switch entry.Provider {
	case "":
		if !m.standalone {
			return func() tea.Msg { return LoginAnilistMsg{} }
		}
		m.LoginAnilist = true
		m.quitting = true
		return tea.Quit
	case mangaPlusID:
		m.inputting = true
		m.secret.SetValue("")
		m.notification = "the device secret of the official app (empty removes it), see the mangoplus docs"
		return m.secret.Focus()
	}
	m.notification = "nothing to log into for " + entry.Service
	return nil
}

func (m *Model) updateSecret(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc:
		m.inputting = false
		m.notification = ""
		m.secret.Blur()
		return nil
	case tea.KeyEnter:
		m.inputting = false
		m.secret.Blur()
		if err := cache.SetMangaPlusSecret(strings.TrimSpace(m.secret.Value())); err != nil {
			m.notification = err.Error()
			return nil
		}
		// the loaded providers keep the secret they started with
		m.notification = "saved, used from the next run of mangal"
		return nil
	}
	var cmd tea.Cmd
	m.secret, cmd = m.secret.Update(msg)
	return cmd
}

// View implements tea.Model.
func (m *Model) View() string {
	if m.quitting {
		return ""
	}
	parts := []string{}
	if m.standalone {
		parts = append(parts, lipgloss.NewStyle().Background(color.Accent).Foreground(color.Background).Padding(0, 1).Render("Mangal Accounts"))
	}
	if m.loading {
		parts = append(parts, style.Normal.Secondary.Render("loading accounts..."))
	} else {
		parts = append(parts, m.table.View())
	}
	if m.inputting {
		parts = append(parts, m.secret.View())
	}
	if m.notification != "" {
		parts = append(parts, style.Normal.Warning.Render(m.notification))
	}
	if m.standalone {
		parts = append(parts, " ", m.help.View(&m.keyMap))
		return style.Normal.Base.Margin(0, 2).Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// KeyMap is the model's keys, for the TUI's help.
func (m *Model) KeyMap() help.KeyMap {
	return &m.keyMap
}

// Inputting reports whether the secret is being typed (keys go to the input).
func (m *Model) Inputting() bool {
	return m.inputting
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

// KeyMap implements help.KeyMap.
type KeyMap struct {
	refresh, login, copy, quit key.Binding
}

func newKeyMap() KeyMap {
	return KeyMap{
		refresh: util.Bind("refresh", "r"),
		login:   util.Bind("log in / set secret", "l"),
		copy:    util.Bind("copy url", "c"),
		quit:    util.Bind("quit", "q", "ctrl+c"),
	}
}

// ShortHelp implements help.KeyMap.
func (k *KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.refresh, k.login, k.copy, k.quit}
}

// FullHelp implements help.KeyMap.
func (k *KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}

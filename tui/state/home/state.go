package home

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/meta"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/accounts"
	"github.com/luevano/mangal/v5/tui/state/library"
	"github.com/luevano/mangal/v5/tui/state/migrate"
	"github.com/luevano/mangal/v5/tui/state/providers"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/luevano/mangal/v5/util/cache"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

var _ base.State = (*state)(nil)

type state struct {
	// providers is kept, it's shown again with its loaded marks
	providers base.State
	// shown again: tui.skip_home only applies at the start
	restored bool

	confirm,
	library,
	accounts,
	migrate key.Binding
}

// loadedMsg are the providers, with what to tell about them.
type loadedMsg struct {
	providers base.State
	// shown again: tui.skip_home only applies at the start
	restored bool
	notices  []string
}

func New() base.State {
	return &state{
		confirm:  util.Bind("providers", "enter"),
		library:  util.Bind("library", "l"),
		accounts: util.Bind("accounts", "a"),
		migrate:  util.Bind("migrate", "M"),
	}
}

func (s *state) Backable() bool                { return false }
func (s *state) Inputting() bool               { return false }
func (s *state) Title() base.Title             { return base.Title{Text: "Home"} }
func (s *state) Subtitle() string              { return "" }
func (s *state) Status() string                { return "" }
func (s *state) Resize(size base.Size) tea.Cmd { return nil }

func (s *state) KeyMap() help.KeyMap {
	return util.KeyMap{Short: []key.Binding{s.confirm, s.library, s.accounts, s.migrate}}
}

func (s *state) Init(ctx context.Context) tea.Cmd {
	return s.load(ctx)
}

func (s *state) load(ctx context.Context) tea.Cmd {
	return base.Load(ctx, "Loading providers", func(context.Context) (loadedMsg, error) {
		loaders, problems, err := manager.Load()
		if err != nil {
			return loadedMsg{}, err
		}
		var notices []string
		if len(problems) != 0 {
			lines := make([]string, len(problems))
			for i, p := range problems {
				log.Warn("skipped %s", p)
				lines[i] = "  " + p.Error()
			}
			notices = append(notices, fmt.Sprintf("Skipped %s that can't be loaded:\n%s", stringutil.Quantify(len(problems), "provider", "providers"), strings.Join(lines, "\n")))
		}
		if _, notice, err := cache.OldIDsNotice(loaders); err == nil && notice != "" {
			notices = append(notices, notice)
		}
		return loadedMsg{providers: providers.New(loaders), notices: notices}, nil
	}, func(msg loadedMsg) tea.Msg { return msg })
}

func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case loadedMsg:
		s.providers = msg.providers
		if len(msg.notices) != 0 {
			return base.Viewport("Notice", strings.Join(msg.notices, "\n\n"), color.Warning)
		}
		if config.TUI.SkipHome.Get() && !s.restored {
			return func() tea.Msg { return s.providers }
		}
		return base.Notify("Providers loaded")
	case base.RestoredMsg:
		s.restored = true
		// left before they loaded
		if s.providers == nil {
			return s.load(ctx)
		}
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, s.confirm):
			if s.providers == nil {
				return base.Notify("Providers not yet loaded")
			}
			return func() tea.Msg { return s.providers }
		case key.Matches(msg, s.library):
			return func() tea.Msg { return library.New() }
		case key.Matches(msg, s.accounts):
			return func() tea.Msg { return accounts.New(false) }
		case key.Matches(msg, s.migrate):
			return func() tea.Msg { return migrate.New() }
		}
	}
	return nil
}

func (s *state) View() string {
	return lipgloss.JoinVertical(lipgloss.Center, style.Bold.Accent.Render(meta.Logo), "\n"+meta.Versions())
}

package home

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/health"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/meta"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/accounts"
	healthstate "github.com/luevano/mangal/v5/tui/state/health"
	"github.com/luevano/mangal/v5/tui/state/history"
	"github.com/luevano/mangal/v5/tui/state/jobkinds"
	"github.com/luevano/mangal/v5/tui/state/library"
	"github.com/luevano/mangal/v5/tui/state/migrate"
	"github.com/luevano/mangal/v5/tui/state/providers"
	"github.com/luevano/mangal/v5/tui/state/watches"
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

	// health is the offline health check's summary line, empty until it ran
	health string

	confirm,
	library,
	watches,
	accounts,
	migrate,
	healthKey,
	history,
	jobs key.Binding
}

// healthMsg is the summary of an offline health check.
type healthMsg struct{ line string }

// loadedMsg are the providers, with what to tell about them.
type loadedMsg struct {
	providers base.State
	notices   []string
}

func New() base.State {
	return &state{
		confirm:   util.Bind("providers", "enter"),
		library:   base.KeyLibrary,
		watches:   base.KeyWatches,
		accounts:  base.KeyAccounts,
		migrate:   base.KeyMigrate,
		healthKey: base.KeyHealth,
		history:   base.KeyReadHistory,
		jobs:      base.KeyJobs,
	}
}

func (s *state) Key() string                   { return "home" }
func (s *state) Backable() bool                { return false }
func (s *state) Inputting() bool               { return false }
func (s *state) Title() base.Title             { return base.Title{Text: "Home"} }
func (s *state) Subtitle() string              { return "" }
func (s *state) Status() string                { return "" }
func (s *state) Resize(size base.Size) tea.Cmd { return nil }

func (s *state) KeyMap() help.KeyMap {
	return util.KeyMap{Short: []key.Binding{s.confirm, s.library, s.watches, s.accounts, s.migrate, s.healthKey, s.history, s.jobs}}
}

func (s *state) Init(ctx context.Context) tea.Cmd {
	return tea.Batch(s.load(ctx), s.checkHealth(ctx))
}

// checkHealth runs the offline health checks (local, cheap) for the summary line.
func (s *state) checkHealth(ctx context.Context) tea.Cmd {
	key := s.healthKey.Help().Key
	return base.Load(ctx, "", func(ctx context.Context) (healthMsg, error) {
		c, err := base.Core()
		if err != nil {
			warnHealth(err)
			return healthMsg{}, nil
		}
		report, err := c.Health.Report(ctx, health.Options{})
		if err != nil {
			warnHealth(err)
			return healthMsg{}, nil
		}
		return healthMsg{line: healthLine(report, key)}, nil
	}, func(msg healthMsg) tea.Msg { return msg })
}

// healthLine is "health: 3 problems (!)": errors and warnings, info isn't a problem.
func healthLine(report health.Report, key string) string {
	n := report.Counts.Error + report.Counts.Warning
	switch n {
	case 0:
		return fmt.Sprintf("health: no problems (%s)", key)
	case 1:
		return fmt.Sprintf("health: 1 problem (%s)", key)
	}
	return fmt.Sprintf("health: %d problems (%s)", n, key)
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
	case healthMsg:
		s.health = msg.line
	case base.RestoredMsg:
		s.restored = true
		// left before they loaded
		if s.providers == nil {
			return tea.Batch(s.load(ctx), s.checkHealth(ctx))
		}
		return s.checkHealth(ctx)
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, s.confirm):
			if s.providers == nil {
				return base.Notify("Providers not yet loaded")
			}
			return func() tea.Msg { return s.providers }
		case key.Matches(msg, s.library):
			return func() tea.Msg { return library.New() }
		case key.Matches(msg, s.watches):
			return func() tea.Msg { return watches.New() }
		case key.Matches(msg, s.accounts):
			return func() tea.Msg { return accounts.New(false) }
		case key.Matches(msg, s.migrate):
			return func() tea.Msg { return migrate.New() }
		case key.Matches(msg, s.healthKey):
			return func() tea.Msg { return healthstate.New() }
		case key.Matches(msg, s.history):
			return func() tea.Msg { return history.New() }
		case key.Matches(msg, s.jobs):
			return func() tea.Msg { return jobkinds.New() }
		}
	}
	return nil
}

func (s *state) View() string {
	view := lipgloss.JoinVertical(lipgloss.Center, style.Bold.Accent.Render(meta.Logo), "\n"+meta.Versions())
	if s.health != "" {
		st := style.Normal.Secondary
		if !strings.HasPrefix(s.health, "health: no ") {
			st = style.Normal.Warning
		}
		view = lipgloss.JoinVertical(lipgloss.Center, view, "\n"+st.Render(s.health))
	}
	return view
}

// warnHealth logs a failed summary check; leaving home (skip_home) cancels it, that's no warning.
func warnHealth(err error) {
	if !errors.Is(err, context.Canceled) {
		log.Warn("health: %s", err)
	}
}

package home

import (
	"context"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/meta"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/state/accounts"
	"github.com/luevano/mangal/v5/tui/state/library"
	"github.com/luevano/mangal/v5/tui/state/providers"
	"github.com/luevano/mangal/v5/util/cache"
)

var _ base.State = (*state)(nil)

// state implements base.state.
type state struct {
	providersState base.State
	keyMap         keyMap
}

// Intermediate implements base.State.
func (s *state) Intermediate() bool {
	return false
}

// Backable implements base.State.
func (s *state) Backable() bool {
	return false
}

// KeyMap implements base.State.
func (s *state) KeyMap() help.KeyMap {
	return s.keyMap
}

// Title implements base.State.
func (s *state) Title() base.Title {
	return base.Title{Text: "Home"}
}

// Subtitle implements base.State.
func (*state) Subtitle() string {
	return ""
}

// Status implements base.State.
func (s *state) Status() string {
	return ""
}

// Resize implements base.State.
func (s *state) Resize(size base.Size) tea.Cmd {
	return nil
}

// Init implements base.State.
func (s *state) Init(ctx context.Context) tea.Cmd {
	var loaders []libmangal.ProviderLoader
	return tea.Sequence(
		base.Loading("Loading providers"),
		func() tea.Msg {
			var err error
			loaders, err = manager.Loaders()
			if err != nil {
				return err
			}
			s.providersState = providers.New(loaders)
			return nil
		},
		base.Loaded,
		func() tea.Msg {
			if _, notice, err := cache.OldIDsNotice(loaders); err == nil && notice != "" {
				return base.Viewport("Notice", notice, color.Warning)()
			}
			if config.TUI.SkipHome.Get() {
				return s.providersState
			}
			return base.Notify("Providers loaded")()
		},
	)
}

// Update implements base.State.
func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, s.keyMap.confirm):
			if s.providersState == nil {
				return base.Notify("Providers not yet loaded")
			}
			return func() tea.Msg {
				return s.providersState
			}
		case key.Matches(msg, s.keyMap.library):
			return func() tea.Msg {
				state, err := library.New()
				if err != nil {
					return err
				}
				return state
			}
		case key.Matches(msg, s.keyMap.accounts):
			return func() tea.Msg {
				return accounts.New(ctx)
			}
		}
	}
	return nil
}

// View implements base.State.
func (s *state) View() string {
	return meta.PrettyVersion()
}

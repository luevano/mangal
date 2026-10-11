// Package accounts is the TUI's accounts view.
package accounts

import (
	"context"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/accounts"
)

var _ base.State = (*state)(nil)

type state struct {
	model *accounts.Model
}

// New is the accounts view.
func New(ctx context.Context) base.State {
	return &state{model: accounts.New(ctx, false)}
}

func (s *state) Backable() bool                { return !s.model.Inputting() }
func (s *state) Inputting() bool               { return s.model.Inputting() }
func (s *state) KeyMap() help.KeyMap           { return s.model.KeyMap() }
func (s *state) Title() base.Title             { return base.Title{Text: "Accounts"} }
func (s *state) Subtitle() string              { return "cached for 12 hours, r fetches them now" }
func (s *state) Status() string                { return "" }
func (s *state) Resize(size base.Size) tea.Cmd { return nil }

func (s *state) Init(ctx context.Context) tea.Cmd {
	s.model.SetContext(ctx)
	return s.model.Init()
}

func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	s.model.SetContext(ctx)
	switch msg.(type) {
	case accounts.LoginAnilistMsg:
		return func() tea.Msg { return newAnilistLogin(ctx) }
	case base.RestoredMsg:
		return s.model.Reload()
	}
	_, cmd := s.model.Update(msg)
	return cmd
}

func (s *state) View() string {
	return s.model.View()
}

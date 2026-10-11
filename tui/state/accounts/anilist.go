package accounts

import (
	"context"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
	anilistLogin "github.com/luevano/mangal/v5/tui/model/anilist"
)

var _ base.State = (*anilistState)(nil)

// anilistState is the Anilist login, opened from the accounts.
type anilistState struct {
	model *anilistLogin.Model
}

func newAnilistLogin(ctx context.Context) base.State {
	return &anilistState{model: anilistLogin.New(ctx, false)}
}

func (s *anilistState) Backable() bool      { return s.model.Backable() }
func (s *anilistState) Inputting() bool     { return !s.model.Backable() }
func (s *anilistState) KeyMap() help.KeyMap { return s.model.KeyMap() }
func (s *anilistState) Title() base.Title {
	return base.Title{Text: "Anilist login", Background: color.Anilist}
}
func (s *anilistState) Subtitle() string { return "" }
func (s *anilistState) Status() string   { return "" }

func (s *anilistState) Resize(size base.Size) tea.Cmd {
	return s.model.Resize(size)
}

func (s *anilistState) Init(ctx context.Context) tea.Cmd {
	return s.model.Init()
}

func (s *anilistState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	_, cmd := s.model.Update(msg)
	return cmd
}

func (s *anilistState) View() string {
	return s.model.View()
}

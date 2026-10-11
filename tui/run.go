package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/state/home"
	"github.com/luevano/mangal/v5/util/cache"
)

func Run() error {
	notices := cache.Notices()
	if notice := anilist.LoginNotice(); notice != "" {
		notices = append(notices, notice)
	}
	model := base.New(home.New(), notices...)
	_, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

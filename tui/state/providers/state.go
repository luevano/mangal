package providers

import (
	"context"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/zyedidia/generic/set"
)

var _ base.State = (*state)(nil)

// state implements base.state.
type state struct {
	list      *list.Model
	loaded    *set.Set[*item]
	extraInfo *bool
	keyMap    keyMap
}

// Backable implements base.State.
func (s *state) Backable() bool {
	return s.list.Unfiltered()
}

// KeyMap implements base.State.
func (s *state) KeyMap() help.KeyMap {
	return base.CombinedKeyMap(s.keyMap, s.list.KeyMap)
}

// Title implements base.State.
func (s *state) Title() base.Title {
	return base.Title{Text: "Providers"}
}

// Subtitle implements base.State.
func (s *state) Subtitle() string {
	return s.list.Subtitle()
}

// Status implements base.State.
func (s *state) Status() string {
	return s.list.Status()
}

// Resize implements base.State.
func (s *state) Resize(size base.Size) tea.Cmd {
	return s.list.Resize(size)
}

// Init implements base.State.
func (s *state) Init(ctx context.Context) tea.Cmd {
	// TODO: decide if Init should close all clients, instead use
	// State.Destroy() (if implemented) method and perform that there?
	return tea.Sequence(
		func() tea.Msg {
			return client.CloseAll()
		},
		s.list.Init(),
	)
}

// Update implements base.State.
func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if s.list.Filtering() {
			goto end
		}

		i, ok := s.list.SelectedItem().(*item)
		if !ok {
			return nil
		}

		switch {
		case key.Matches(msg, s.keyMap.confirm):
			return s.loadProviderCmd(ctx, i)
		case key.Matches(msg, s.keyMap.info):
			*s.extraInfo = !(*s.extraInfo)

			if *s.extraInfo {
				s.list.SetItemHeight(3)
			} else {
				s.list.SetItemHeight(2)
			}
		case key.Matches(msg, s.keyMap.closeAll):
			return s.closeAllProvidersCmd
		case key.Matches(msg, s.keyMap.mirrors):
			info := i.loader.Info()
			if len(info.Mirrors) == 0 {
				return base.Notify(info.Name + " has no mirrors")
			}
			return func() tea.Msg {
				return newMirrors(info, s.closeAll)
			}
		}
	}
end:
	return s.list.Update(msg)
}

// View implements base.State.
func (s *state) View() string {
	return s.list.View()
}

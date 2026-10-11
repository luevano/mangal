package anilist

import (
	"context"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/libmangal/mangadata"
	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/model/metadata"
	"github.com/luevano/mangal/v5/tui/model/search"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/luevano/mangal/v5/util/cache"
)

var _ base.State = (*state)(nil)

// state implements base.state.
type state struct {
	list    *list.Model
	search  *search.Model
	manga   mangadata.Manga
	anilist *lmmeta.ProviderWithCache

	history  cache.Records
	searched bool

	keyMap keyMap
}

// Backable implements base.State.
func (s *state) Backable() bool {
	return s.list.Unfiltered() && !s.search.Searching()
}

// Inputting implements base.State.
func (s *state) Inputting() bool {
	return s.list.Filtering() || s.search.Searching()
}

// KeyMap implements base.State.
func (s *state) KeyMap() help.KeyMap {
	return base.CombinedKeyMap(s.keyMap, s.list.KeyMap)
}

// Title implements base.State.
func (s *state) Title() base.Title {
	return base.Title{
		Text:       "Anilist Search",
		Background: color.Anilist,
		Foreground: color.Bright,
	}
}

// Subtitle implements base.State.
func (s *state) Subtitle() string {
	if !s.searched {
		return ""
	}
	return s.list.Subtitle()
}

// Status implements base.State.
func (s *state) Status() string {
	return s.list.Status()
}

// Resize implements base.State.
func (s *state) Resize(size base.Size) tea.Cmd {
	s.search.Resize(size)
	_, searchHeight := lipgloss.Size(s.search.View())
	size.Height -= searchHeight + 1 // +1 for added padding

	return s.list.Resize(size)
}

// Init implements base.State.
func (s *state) Init(ctx context.Context) tea.Cmd {
	return tea.Sequence(
		s.getHistoryCmd,
		s.searchCmd(ctx, s.search.Query()),
		s.search.Init(),
		s.list.Init(),
	)
}

// Updateimplements base.State.
func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if s.list.Filtering() || s.search.Searching() {
			goto end
		}

		// don't return on nil item, keybinds will be disabled for relevant actions
		i, _ := s.list.SelectedItem().(*item)
		switch {
		case key.Matches(msg, s.keyMap.confirm):
			return s.setMetadataCmd(i.meta)
		case key.Matches(msg, s.keyMap.search):
			s.list.ResetFilter()
			return s.search.Focus()
		case key.Matches(msg, s.keyMap.metadata):
			return metadata.New(i.meta).ShowMetadataCmd()
		}
	case search.SearchMsg:
		return tea.Sequence(
			s.updateHistoryCmd(string(msg)),
			s.searchCmd(ctx, string(msg)),
		)
	}
end:
	if s.search.Searching() {
		return s.search.Update(msg)
	}
	return s.list.Update(msg)
}

// View implements base.State.
func (s *state) View() string {
	if !s.searched {
		return s.search.View() // no need to display suggestions
	}

	if s.search.Searching() {
		input := s.search.View()
		view := lipgloss.JoinVertical(
			lipgloss.Left,
			input,
			" ", // "padding" bottom of input
			s.list.View(),
		)
		h := lipgloss.Height(input)
		return util.PlaceOverlay(0, h, s.search.SuggestionBox(), view)
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		s.search.View(),
		" ", // "padding" bottom of input
		s.list.View(),
	)
}

func (s *state) updateKeybinds() {
	enable := len(s.list.Items()) != 0
	s.keyMap.confirm.SetEnabled(enable)
	s.keyMap.metadata.SetEnabled(enable)
}

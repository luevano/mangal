package anilistmangas

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	lmanilist "github.com/luevano/libmangal/metadata/anilist"
	"github.com/luevano/mangal/tui/base"
	"github.com/luevano/mangal/tui/state/wrapper/list"
	"github.com/luevano/mangal/tui/state/wrapper/textinput"
)

var _ base.State = (*State)(nil)

type OnResponseFunc func(response *lmanilist.Manga) tea.Cmd

// State implements base.State.
type State struct {
	anilist *lmanilist.Anilist
	list    *list.State

	onResponse OnResponseFunc

	keyMap keyMap
}

// Intermediate implements base.State.
func (s *State) Intermediate() bool {
	return true
}

// Backable implements base.State.
func (s *State) Backable() bool {
	return s.list.Backable()
}

// KeyMap implements base.State.
func (s *State) KeyMap() help.KeyMap {
	return s.list.KeyMap()
}

// Title implements base.State.
func (s *State) Title() base.Title {
	return base.Title{Text: "Anilist Mangas"}
}

// Subtitle implements base.State.
func (s *State) Subtitle() string {
	return s.list.Subtitle()
}

// Status implements base.State.
func (s *State) Status() string {
	return s.list.Status()
}

// Resize implements base.State.
func (s *State) Resize(size base.Size) tea.Cmd {
	return s.list.Resize(size)
}

// Init implements base.State.
func (s *State) Init(ctx context.Context) tea.Cmd {
	return s.list.Init(ctx)
}

// Updateimplements base.State.
func (s *State) Update(ctx context.Context, msg tea.Msg) (cmd tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if s.list.FilterState() == _list.Filtering {
			goto end
		}

		switch {
		case key.Matches(msg, s.keyMap.confirm):
			item, ok := s.list.SelectedItem().(Item)
			if !ok {
				return nil
			}

			return s.onResponse(item.Manga)
		case key.Matches(msg, s.keyMap.search):
			return func() tea.Msg {
				return textinput.New(textinput.Options{
					Title:        base.Title{Text: "Search Anilist"},
					Subtitle:     "Search Anilist manga",
					Placeholder:  "Anilist manga title...",
					Intermediate: true,
					OnResponse: func(response string) tea.Cmd {
						return tea.Sequence(
							base.Loading(fmt.Sprintf("Searching %q on Anilist", response)),
							func() tea.Msg {
								mangas, err := s.anilist.SearchMangas(ctx, response)
								if err != nil {
									return err
								}

								return New(s.anilist, mangas, s.onResponse)
							},
							base.Loaded,
						)
					},
				})
			}
		}
	}

end:
	return s.list.Update(ctx, msg)
}

// View implements base.State.
func (s *State) View() string {
	return s.list.View()
}

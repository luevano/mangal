package anilistmangas

import (
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	lmanilist "github.com/luevano/libmangal/metadata/anilist"
	"github.com/luevano/mangal/tui/base"
	"github.com/luevano/mangal/tui/state/listwrapper"
	"github.com/luevano/mangal/tui/state/loading"
	"github.com/luevano/mangal/tui/state/textinput"
)

var _ base.State = (*State)(nil)

type OnResponseFunc func(response *lmanilist.Manga) tea.Cmd

// State implements base.State.
type State struct {
	anilist *lmanilist.Anilist
	list    *listwrapper.State

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
	return s.keyMap
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
func (s *State) Resize(size base.Size) {
	s.list.Resize(size)
}

// Init implements base.State.
func (s *State) Init(model base.Model) tea.Cmd {
	return s.list.Init(model)
}

// Updateimplements base.State.
func (s *State) Update(model base.Model, msg tea.Msg) (cmd tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if s.list.FilterState() == list.Filtering {
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
							func() tea.Msg {
								return loading.New("Searching", fmt.Sprintf("Searching for %q on Anilist", response))
							},
							func() tea.Msg {
								mangas, err := s.anilist.SearchMangas(model.Context(), response)
								if err != nil {
									return err
								}

								return New(s.anilist, mangas, s.onResponse)
							},
						)
					},
				})
			}
		}
	}

end:
	return s.list.Update(model, msg)
}

// View implements base.State.
func (s *State) View(model base.Model) string {
	return s.list.View(model)
}

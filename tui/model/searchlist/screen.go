// Package searchlist is a search input over a list of results, with the search history
// as suggestions, for the search screens (mangas, anilist).
package searchlist

import (
	"context"

	"github.com/charmbracelet/bubbles/help"
	_list "github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/model/search"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/luevano/mangal/v5/util/cache"
)

// History reads and saves a search history, e.g. cache.GetMangasSearchHistory and
// cache.SetMangasSearchHistory.
type History struct {
	Get func(*cache.Records) (bool, error)
	Set func(cache.Records) error
}

// Screen is embedded in a search base.State, it implements all but Title, KeyMap and
// Update. The State starts its search on a search.SearchMsg (numbered by NewSearch),
// shows the results of the latest one with SetResults, and sends every msg to
// Screen.Update (which keeps the history):
//
//	func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
//		switch msg := msg.(type) {
//		case search.SearchMsg:
//			n := s.NewSearch()
//			return tea.Batch(s.Screen.Update(msg), base.Load(ctx, "Searching", ..., toResultsMsg(n)))
//		case resultsMsg:
//			if !s.Latest(msg.search) {
//				return nil
//			}
//			return s.SetResults(msg.items)
//		case tea.KeyMsg:
//			if !s.Inputting() {
//				// the state's keys
//			}
//		}
//		return s.Screen.Update(msg)
//	}
type Screen struct {
	Search *search.Model
	List   *list.Model

	history  History
	records  cache.Records
	searched bool
	// searches counts the searches started, results of an older one are dropped
	searches int
}

// New makes a Screen, the history (its funcs may be nil) gives the suggestions.
func New(search *search.Model, list *list.Model, history History) Screen {
	return Screen{Search: search, List: list, history: history}
}

type historyMsg struct {
	screen  *Screen
	records cache.Records
}

// NewSearch numbers a search started now, its results carry the number to Latest.
func (s *Screen) NewSearch() int {
	s.searches++
	return s.searches
}

// Latest is true for the results of the latest search: an older one's that comes late
// (a slow search, then another) would replace them.
func (s *Screen) Latest(search int) bool {
	return search == s.searches
}

// Searched is true once results were set.
func (s *Screen) Searched() bool {
	return s.searched
}

// SetResults shows the items of a search.
func (s *Screen) SetResults(items []_list.Item) tea.Cmd {
	s.searched = true
	return s.List.SetItems(items)
}

// Backable implements base.State.
func (s *Screen) Backable() bool {
	return s.List.Unfiltered() && !s.Search.Searching()
}

// Inputting implements base.State.
func (s *Screen) Inputting() bool {
	return s.List.Filtering() || s.Search.Searching()
}

// ListKeyMap is the list's keys, for the State's KeyMap.
func (s *Screen) ListKeyMap() help.KeyMap {
	return s.List.KeyMap
}

// Subtitle implements base.State, the results count once searched.
func (s *Screen) Subtitle() string {
	if !s.searched {
		return ""
	}
	return s.List.Subtitle()
}

// Status implements base.State.
func (s *Screen) Status() string {
	return s.List.Status()
}

// Resize implements base.State.
func (s *Screen) Resize(size base.Size) tea.Cmd {
	s.Search.Resize(size)
	// the input and a blank line
	size.Height -= lipgloss.Height(s.Search.View()) + 1
	return s.List.Resize(size)
}

// Init implements base.State: it reads the history.
func (s *Screen) Init(ctx context.Context) tea.Cmd {
	cmds := []tea.Cmd{s.Search.Init()}
	if get := s.history.Get; get != nil {
		cmds = append(cmds, base.Load(ctx, "", func(context.Context) (cache.Records, error) {
			var records cache.Records
			_, err := get(&records)
			return records, err
		}, func(records cache.Records) tea.Msg {
			return historyMsg{screen: s, records: records}
		}))
	}
	return tea.Batch(cmds...)
}

// Update handles the history, then the search input while searching, else the list.
func (s *Screen) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case historyMsg:
		if msg.screen == s {
			s.records = msg.records
			s.Search.SetSuggestions(s.records.Get())
		}
		return nil
	case search.SearchMsg:
		return s.addHistory(string(msg))
	}
	if s.Search.Searching() {
		return s.Search.Update(msg)
	}
	return s.List.Update(msg)
}

func (s *Screen) addHistory(query string) tea.Cmd {
	s.records.Add(query)
	s.records.Sort()
	s.Search.SetSuggestions(s.records.Get())

	set := s.history.Set
	if set == nil {
		return nil
	}
	// a copy, Add changes the records' ranks
	records := make(cache.Records, len(s.records))
	for i, r := range s.records {
		r := *r
		records[i] = &r
	}
	return func() tea.Msg {
		if err := set(records); err != nil {
			return err
		}
		return nil
	}
}

// View implements base.State: the input, with the suggestions over the results while
// searching.
func (s *Screen) View() string {
	input := s.Search.View()
	if !s.searched {
		if !s.Search.Searching() {
			return input
		}
		return lipgloss.JoinVertical(lipgloss.Left, input, s.Search.SuggestionBox())
	}

	view := lipgloss.JoinVertical(lipgloss.Left, input, " ", s.List.View())
	if !s.Search.Searching() {
		return view
	}
	return util.PlaceOverlay(0, lipgloss.Height(input), s.Search.SuggestionBox(), view)
}

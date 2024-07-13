package list

import (
	"context"
	"slices"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/tui/base"
	stringutil "github.com/luevano/mangal/util/string"
)

var _ base.State = (*State)(nil)

// State implements base.State. Wrapper of list.Model.
type State struct {
	list     list.Model
	delegate *list.DefaultDelegate
	keyMap   keyMap
}

// Intermediate implements base.State.
func (s *State) Intermediate() bool {
	return false
}

// Backable implements base.State.
func (s *State) Backable() bool {
	return s.FilterState() == list.Unfiltered
}

// KeyMap implements base.State.
func (s *State) KeyMap() help.KeyMap {
	return s.keyMap
}

// Title implements base.State.
func (s *State) Title() base.Title {
	return base.Title{Text: "List"}
}

// Subtitle implements base.State.
func (s *State) Subtitle() string {
	singular, plural := s.list.StatusBarItemName()
	return stringutil.Quantify(len(s.list.VisibleItems()), singular, plural)
}

// Status implements base.State.
func (s *State) Status() string {
	var p string
	if len(s.Items()) != 0 {
		p = s.list.Paginator.View()
	}

	if s.FilterState() == list.Filtering || s.list.FilterValue() != "" {
		return s.list.Paginator.View() + " " + s.list.FilterInput.View()
	}

	return p
}

// Resize implements base.State. Wrapper of list.Model.
func (s *State) Resize(size base.Size) tea.Cmd {
	s.list.SetSize(size.Width, size.Height)
	return nil
}

// Init implements base.State.
func (s *State) Init(ctx context.Context) tea.Cmd {
	return nil
}

// Update implements base.State.
func (s *State) Update(ctx context.Context, msg tea.Msg) (cmd tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if s.FilterState() == list.Filtering {
			goto end
		}

		switch {
		case key.Matches(msg, s.keyMap.reverse):
			slices.Reverse(s.Items())
			return tea.Sequence(
				s.list.SetItems(s.Items()),
				base.Notify("Reversed"),
			)
		}
	}

end:
	s.list, cmd = s.list.Update(msg)
	return cmd
}

// View implements base.State. Wrapper of list.Model.
func (s *State) View() string {
	return s.list.View()
}

// FilterState is a wrapper of list.Model.
func (s *State) FilterState() list.FilterState {
	return s.list.FilterState()
}

// ResetFilter is a wrapper of list.Model.
func (s *State) ResetFilter() {
	s.list.ResetFilter()
}

// SelectedItem is a wrapper of list.Model.
func (s *State) SelectedItem() list.Item {
	return s.list.SelectedItem()
}

// Items is a wrapper of list.Model.
func (s *State) Items() []list.Item {
	return s.list.Items()
}

// SetItems is a wrapper of list.Model.
func (s *State) SetItems(items []list.Item) tea.Cmd {
	s.updateKeybinds(len(items) != 0)
	s.list.ResetSelected()
	return s.list.SetItems(items)
}

// SetItem is a wrapper of list.Model.
func (s *State) SetItem(index int, item list.Item) tea.Cmd {
	return s.list.SetItem(index, item)
}

// InsertItem is a wrapper of list.Model.
func (s *State) InsertItem(index int, item list.Item) tea.Cmd {
	return s.list.InsertItem(index, item)
}

// RemoveItem is a wrapper of list.Model.
func (s *State) RemoveItem(index int) {
	s.list.RemoveItem(index)
}

// SetDelegateHeight sets the height of the delegate, which translates to the items' height.
//
// Clamps to a minimum of 1, in which case the description is hidden.
func (s *State) SetDelegateHeight(height int) {
	if height < 2 {
		height = 1
	}
	if height == 1 {
		s.delegate.ShowDescription = false
	} else {
		s.delegate.ShowDescription = true
	}
	s.delegate.SetHeight(height)
	s.list.SetDelegate(s.delegate)
}

// updateKeybinds will enable/disable relevant keys.
func (s *State) updateKeybinds(enable bool) {
	s.keyMap.reverse.SetEnabled(enable)
}

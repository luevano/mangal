package list

import (
	"context"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/confirm"
)

// Screen is a list screen to embed in a base.State, it implements all but Title and
// Update:
//
//	type state struct {
//		list.Screen
//		keys keyMap
//	}
//
//	func New() *state {
//		s := &state{keys: newKeyMap()}
//		s.Screen = list.NewScreen(list.New(...), s.keyMap)
//		return s
//	}
//
//	func (s *state) Title() base.Title { return base.Title{Text: "Things"} }
//
//	func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
//		if msg, ok := msg.(tea.KeyMsg); ok && !s.List.Filtering() {
//			// the state's keys
//		}
//		return s.List.Update(msg)
//	}
//
// A Screen with a Confirm shows it over the list while it's asking: it takes every key
// (the State's Update starts with Answer), esc included, and the help is its.
//
// Override any method that needs more (e.g. Inputting with a form open).
type Screen struct {
	List *Model
	// Confirm is the screen's dialog, nil for none
	Confirm *confirm.Dialog
	// keys are the State's keys, shown before the list's
	keys func() help.KeyMap
}

// NewScreen makes a Screen of list, keys (may be nil) returns the State's keys.
func NewScreen(list *Model, keys func() help.KeyMap) Screen {
	return Screen{List: list, keys: keys}
}

// Asking is true while the Confirm is shown.
func (s *Screen) Asking() bool { return s.Confirm != nil && s.Confirm.Active() }

// Answer gives a key msg to the Confirm while it's asking, ok is false when it isn't (the
// msg is the State's).
func (s *Screen) Answer(msg tea.Msg) (cmd tea.Cmd, ok bool) {
	if msg, isKey := msg.(tea.KeyMsg); isKey && s.Asking() {
		return s.Confirm.Update(msg), true
	}
	return nil, false
}

// Backable implements base.State: esc stops filtering first, or answers the Confirm.
func (s *Screen) Backable() bool { return s.List.Unfiltered() && !s.Asking() }

// Inputting implements base.State.
func (s *Screen) Inputting() bool { return s.List.Filtering() || s.Asking() }

// KeyMap implements base.State.
func (s *Screen) KeyMap() help.KeyMap {
	if s.Asking() {
		return s.Confirm.KeyMap()
	}
	if s.keys == nil {
		return s.List.KeyMap
	}
	return base.CombinedKeyMap(s.keys(), s.List.KeyMap)
}

// Subtitle implements base.State.
func (s *Screen) Subtitle() string { return s.List.Subtitle() }

// Status implements base.State.
func (s *Screen) Status() string { return s.List.Status() }

// Resize implements base.State.
func (s *Screen) Resize(size base.Size) tea.Cmd {
	if s.Confirm != nil {
		s.Confirm.Resize(size)
	}
	return s.List.Resize(size)
}

// Init implements base.State.
func (s *Screen) Init(context.Context) tea.Cmd { return nil }

// View implements base.State.
func (s *Screen) View() string {
	if s.Asking() {
		return s.Confirm.View()
	}
	return s.List.View()
}

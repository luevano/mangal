package list

import (
	"context"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/tui/base"
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
// Override any method that needs more (e.g. Inputting with a dialog open).
type Screen struct {
	List *Model
	// keys are the State's keys, shown before the list's
	keys func() help.KeyMap
}

// NewScreen makes a Screen of list, keys (may be nil) returns the State's keys.
func NewScreen(list *Model, keys func() help.KeyMap) Screen {
	return Screen{List: list, keys: keys}
}

// Backable implements base.State: esc stops filtering first.
func (s *Screen) Backable() bool { return s.List.Unfiltered() }

// Inputting implements base.State.
func (s *Screen) Inputting() bool { return s.List.Filtering() }

// KeyMap implements base.State.
func (s *Screen) KeyMap() help.KeyMap {
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
func (s *Screen) Resize(size base.Size) tea.Cmd { return s.List.Resize(size) }

// Init implements base.State.
func (s *Screen) Init(context.Context) tea.Cmd { return nil }

// View implements base.State.
func (s *Screen) View() string { return s.List.View() }

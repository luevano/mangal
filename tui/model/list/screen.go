package list

import (
	"context"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
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
//		if msg, ok := msg.(tea.KeyPressMsg); ok && !s.List.Filtering() {
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
	// Host holds the screen's Confirm, nil for none
	confirm.Host
	// keys are the State's keys, shown before the list's
	keys func() help.KeyMap
}

// NewScreen makes a Screen of list, keys (may be nil) returns the State's keys.
func NewScreen(list *Model, keys func() help.KeyMap) Screen {
	return Screen{List: list, keys: keys}
}

// Backable implements base.State: esc stops filtering first, or answers the Confirm.
func (s *Screen) Backable() bool { return s.Host.Backable(s.List.Unfiltered()) }

// Inputting implements base.State.
func (s *Screen) Inputting() bool { return s.Host.Inputting(s.List.Filtering()) }

// KeyMap implements base.State.
func (s *Screen) KeyMap() help.KeyMap {
	if s.keys == nil {
		return s.Host.KeyMap(s.List.KeyMap)
	}
	return s.Host.KeyMap(base.CombinedKeyMap(s.keys(), s.List.KeyMap))
}

// Subtitle implements base.State.
func (s *Screen) Subtitle() string { return s.List.Subtitle() }

// Status implements base.State.
func (s *Screen) Status() string { return s.List.Status() }

// Resize implements base.State.
func (s *Screen) Resize(size base.Size) tea.Cmd { return s.Host.Resize(size, s.List.Resize) }

// Init implements base.State.
func (s *Screen) Init(context.Context) tea.Cmd { return nil }

// View implements base.State.
func (s *Screen) View() string { return s.Host.View(s.List.View) }

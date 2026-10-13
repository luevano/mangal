package base

import (
	"context"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
)

// State is a screen of the TUI, shown one at a time: pushing a State (returning it as a
// tea.Msg) shows it, going back pops it and shows the previous one.
//
// The ctx given to Init and Update is cancelled when the State stops being the shown one
// (another is pushed over it, it's popped or the TUI quits). A State shown again gets a
// RestoredMsg with a new ctx, it restarts there what it needs (e.g. a reload).
type State interface {
	// Backable if esc goes back to the previous State. A State uses esc itself while it
	// isn't, e.g. to stop filtering a list.
	Backable() bool

	// Inputting if the user is typing in the State (a search, a list filter, a form), the
	// printable global keys (H home, ? help, q quit when standalone) go to the State then.
	Inputting() bool

	// KeyMap returns the State specific KeyMap.
	KeyMap() help.KeyMap

	// Title of the State, with optional Background and Foreground.
	Title() Title

	// Subtitle of the State, shown below the Title.
	Subtitle() string

	// Status of the State, shown to the right of the Title.
	Status() string

	// Resize the State's usable viewport.
	//
	// Gets called at least once after Init, as well as each time the State
	// is popped out of the history before Update.
	Resize(size Size) tea.Cmd

	// Init is called when the State is pushed. It returns an optional initial command.
	Init(ctx context.Context) tea.Cmd

	// Update is called when a message is received. Use it to inspect messages
	// and, in response, update the State and/or send a command.
	Update(ctx context.Context, msg tea.Msg) tea.Cmd

	// View renders the State.
	View() string
}

// Keyed is a State with an identity: two States with the same Key are the same window,
// and the history holds one of each. Pushing a State whose Key is in the history goes
// back to that one instead (the States over it are popped), it gets a RestoredMsg with
// the pushed State as the Request. States without it (forms, pickers, a download) are
// always pushed.
//
// Singleton screens (home, library, watches...) have a fixed key; per-item screens (a
// manga's chapters, a watch's details) add the item: "library/42", "watch/7".
type Keyed interface {
	Key() string
}

// Keeper is a State whose work stops when it's left (a running download): going back
// over it (H, a push of a window under it) stops on it with the notice, nothing is
// pushed over it (its msgs would go to that State), it asks itself before it goes (it
// isn't Backable then).
type Keeper interface {
	// Keeping is why it can't be left now (e.g. "a download is running, esc stops it"),
	// empty when it can.
	Keeping() string
}

func keyOf(s State) string {
	if k, ok := s.(Keyed); ok {
		return k.Key()
	}
	return ""
}

func keeping(s State) string {
	if k, ok := s.(Keeper); ok {
		return k.Keeping()
	}
	return ""
}

// Destroyer is a State with something to tear down (a client to release, a watcher to
// stop). Destroy is called when the State is popped, or the TUI quits with it in the
// history.
type Destroyer interface {
	Destroy()
}

func destroy(s State) {
	if d, ok := s.(Destroyer); ok {
		d.Destroy()
	}
}

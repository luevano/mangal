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
// (another is pushed over it, it's popped, the user jumps to another window or the TUI
// quits). A State shown again gets a
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
// and the windows (history, shown, ahead) hold one of each. Pushing a State whose Key is
// open shows that one instead (the windows over it stay, as windows ahead), it gets a
// RestoredMsg with the pushed State as the Request. States without it (forms, pickers)
// are always pushed.
//
// Singleton screens (home, library, watches...) have a fixed key; per-item screens (a
// manga's chapters, a watch's details) add the item: "library/42", "watch/7".
type Keyed interface {
	Key() string
}

func keyOf(s State) string {
	if k, ok := s.(Keyed); ok {
		return k.Key()
	}
	return ""
}

// Destroyer is a State with something to tear down (a client to release, a watcher to
// stop). Destroy is called when the window is closed (popped, or dropped from the
// windows ahead), or the TUI quits with it open.
type Destroyer interface {
	Destroy()
}

func destroy(s State) {
	if d, ok := s.(Destroyer); ok {
		d.Destroy()
	}
}

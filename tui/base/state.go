package base

import (
	"context"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
)

// State is kind of an indirect wrapper interface of Model (wrapper of tea.Model).
//
// State could be thought of as a "Window".
type State interface {
	// Backable if can back out of the State to previous states.
	//
	// For example when filtering a list, this can avoid to go back
	// to previous state and instead cancel the filtering.
	Backable() bool

	// KeyMap returns the State specific KeyMap.
	KeyMap() help.KeyMap

	// Title of the State, with optional Background and Foreground.
	Title() Title

	// Subtitle of the Sate, shown below the Title.
	Subtitle() string

	// Status of the State, shown to the right of the Title.
	Status() string

	// Resize the State's usable viewport.
	//
	// Gets called at least once after Init, as well as each time the State
	// is popped out of the history before Update.
	Resize(size Size) tea.Cmd

	//
	// tea.Model method wrappers.
	//

	// Init is the first function that will be called. It returns an optional
	// initial command. To not perform an initial command return nil.
	Init(ctx context.Context) tea.Cmd

	// Update is called when a message is received. Use it to inspect messages
	// and, in response, update the model and/or send a command.
	Update(ctx context.Context, msg tea.Msg) tea.Cmd

	// View renders the program's UI, which is just a string. The view is
	// rendered after every Update.
	View() string
}

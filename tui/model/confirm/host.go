package confirm

import (
	"image/color"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/tui/base"
)

// Host is a State's dialog: while it asks it takes every key (esc too), its help is shown
// and it's drawn over the State's view. Embed it, start Update with Answer and pass the
// State's own Backable, Inputting, KeyMap, Resize and View through it (list.Screen does
// it for the list screens):
//
//	type state struct {
//		*pick.Manga
//		confirm.Host
//	}
//
//	func (s *state) Backable() bool             { return s.Host.Backable(s.Manga.Backable()) }
//	func (s *state) Inputting() bool            { return s.Host.Inputting(s.Manga.Inputting()) }
//	func (s *state) KeyMap() help.KeyMap        { return s.Host.KeyMap(s.Manga.KeyMap()) }
//	func (s *state) Resize(size base.Size) tea.Cmd { return s.Host.Resize(size, s.Manga.Resize) }
//	func (s *state) View() string               { return s.Host.View(s.Manga.View) }
type Host struct {
	// Confirm is the dialog, nil for none
	Confirm *Dialog
}

// NewHost is a Host with a dialog width wide, in color.
func NewHost(width int, color color.Color) Host {
	return Host{Confirm: NewDialog(width, color)}
}

// Asking is true while the dialog is shown.
func (h *Host) Asking() bool { return h.Confirm != nil && h.Confirm.Active() }

// Answer gives a key msg to the dialog while it's asking, ok is false when it isn't (the
// msg is the State's).
func (h *Host) Answer(msg tea.Msg) (cmd tea.Cmd, ok bool) {
	if msg, isKey := msg.(tea.KeyPressMsg); isKey && h.Asking() {
		return h.Confirm.Update(msg), true
	}
	return nil, false
}

// Backable is the State's backable, false while asking: esc answers the dialog.
func (h *Host) Backable(state bool) bool { return state && !h.Asking() }

// Inputting is the State's inputting, true while asking: every key is the dialog's.
func (h *Host) Inputting(state bool) bool { return state || h.Asking() }

// KeyMap is the dialog's keys while asking, else the State's.
func (h *Host) KeyMap(state help.KeyMap) help.KeyMap {
	if h.Asking() {
		return h.Confirm.KeyMap()
	}
	return state
}

// Resize sizes the dialog as the State's view, then resizes the State.
func (h *Host) Resize(size base.Size, state func(base.Size) tea.Cmd) tea.Cmd {
	if h.Confirm != nil {
		h.Confirm.Resize(size)
	}
	if state == nil {
		return nil
	}
	return state(size)
}

// View is the dialog over the State's view while asking, else the State's view.
func (h *Host) View(state func() string) string {
	if h.Asking() {
		return h.Confirm.View()
	}
	return state()
}

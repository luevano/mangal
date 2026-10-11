package confirm

import (
	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/util"
)

// Dialog asks before doing something, drawn over the State's view. While it's Active, a
// State sends it the key msgs, reports Backable false and Inputting true (esc and every
// key are the dialog's):
//
//	if s.confirm.Active() {
//		if msg, ok := msg.(tea.KeyMsg); ok {
//			return s.confirm.Update(msg)
//		}
//	}
type Dialog struct {
	model  *Model
	active bool
	onYes  tea.Cmd
	behind string
	size   base.Size
	box    lipgloss.Style
}

// NewDialog makes a dialog with a message width wide, in color.
func NewDialog(width int, color lipgloss.Color) *Dialog {
	return &Dialog{
		model: New(width, color),
		box:   lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(color).Padding(0, 1),
	}
}

// Ask shows the dialog over behind (the State's view), onYes runs on y or enter.
func (d *Dialog) Ask(title, message, behind string, onYes tea.Cmd) {
	d.AskWith([]string{"y", "enter"}, title, message, behind, onYes)
}

// AskDelete also confirms with d, the key that started it (d then d), but not with enter.
func (d *Dialog) AskDelete(title, message, behind string, onYes tea.Cmd) {
	d.AskWith([]string{"d", "y"}, title, message, behind, onYes)
}

// AskWith confirms with the yes keys.
func (d *Dialog) AskWith(yes []string, title, message, behind string, onYes tea.Cmd) {
	d.model.SetYesKeys(yes...)
	d.model.SetData(title, message)
	d.active, d.onYes, d.behind = true, onYes, behind
}

// Active is true while the dialog is shown.
func (d *Dialog) Active() bool {
	return d.active
}

// Resize sets the size of the State's view, the dialog is centered in it.
func (d *Dialog) Resize(size base.Size) tea.Cmd {
	d.size = size
	return nil
}

// Update handles the keys while active: yes closes it and runs onYes, no closes it.
func (d *Dialog) Update(msg tea.Msg) tea.Cmd {
	if !d.active {
		return nil
	}
	switch d.model.answer(msg) {
	case answerYes:
		d.active = false
		return d.onYes
	case answerNo:
		d.active = false
	}
	return nil
}

// KeyMap is the dialog's keys.
func (d *Dialog) KeyMap() help.KeyMap {
	return d.model.keyMap
}

// View draws the dialog over what was behind it when asked.
func (d *Dialog) View() string {
	return util.PlaceCenter(d.size.Width, d.size.Height, d.box.Render(d.model.View()), d.behind)
}

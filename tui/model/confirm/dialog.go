package confirm

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
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
	// choices replace yes and no, see Choose
	choices []Choice
	behind  string
	size    base.Size
	box     lipgloss.Style
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
	d.AskWith(append(base.KeyDelete.Keys(), "y"), title, message, behind, onYes)
}

// AskWith confirms with the yes keys.
func (d *Dialog) AskWith(yes []string, title, message, behind string, onYes tea.Cmd) {
	d.model.setChoices(nil)
	d.model.SetYesKeys(yes...)
	d.model.keyMap.scroll, _, _, _ = scrollKeys(d.answers())
	d.model.SetData(title, message)
	d.active, d.onYes, d.choices, d.behind = true, onYes, nil, behind
}

// Choice is an answer of Choose: its key runs its Cmd.
type Choice struct {
	Key   string
	Label string
	Cmd   tea.Cmd
}

// Choose asks to pick one of the choices, esc cancels.
func (d *Dialog) Choose(title, message, behind string, choices ...Choice) {
	bindings := make([]key.Binding, len(choices))
	for i, c := range choices {
		bindings[i] = util.Bind(c.Label, c.Key)
	}
	d.model.setChoices(bindings)
	d.model.keyMap.scroll, _, _, _ = scrollKeys(d.answers())
	d.model.SetData(title, message)
	d.active, d.onYes, d.choices, d.behind = true, nil, choices, behind
}

// Active is true while the dialog is shown.
func (d *Dialog) Active() bool {
	return d.active
}

// Resize sets the size of the State's view, the dialog is centered in it; a message
// taller than what fits scrolls.
func (d *Dialog) Resize(size base.Size) tea.Cmd {
	d.size = size
	// the border, title, blank lines, scroll line and help, and a line of margin each side
	d.model.setHeight(max(3, size.Height-10))
	return nil
}

// answers are the keys that answer the dialog.
func (d *Dialog) answers() []key.Binding {
	k := d.model.keyMap
	return append([]key.Binding{k.yes, k.no}, k.choices...)
}

// scrolled handles the scroll keys of an overflowing message.
func (d *Dialog) scrolled(msg tea.Msg) bool {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok || !d.model.overflows() {
		return false
	}
	up, down, pageUp, pageDown := scrollKeys(d.answers())
	page := max(1, d.model.height-1)
	switch {
	case key.Matches(keyMsg, up):
		d.model.scroll(-1)
	case key.Matches(keyMsg, down):
		d.model.scroll(1)
	case key.Matches(keyMsg, pageUp):
		d.model.scroll(-page)
	case key.Matches(keyMsg, pageDown):
		d.model.scroll(page)
	default:
		return false
	}
	return true
}

// Update handles the keys while active: yes closes it and runs onYes, no closes it.
func (d *Dialog) Update(msg tea.Msg) tea.Cmd {
	if !d.active || d.scrolled(msg) {
		return nil
	}
	if len(d.choices) != 0 {
		msg, ok := msg.(tea.KeyMsg)
		if !ok {
			return nil
		}
		for i, b := range d.model.keyMap.choices {
			if key.Matches(msg, b) {
				d.active = false
				return d.choices[i].Cmd
			}
		}
		if key.Matches(msg, d.model.keyMap.no) {
			d.active = false
		}
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

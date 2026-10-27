// Package huhform is the TUI's forms on huh (charm.land/huh/v2): huh's fields in a
// huh.Form embedded in a screen, with mangal's layout (a label column, a hint and the error
// under each field), keys (tui/base) and theme (theme/huhtheme).
//
// Unlike a plain huh form, enter submits from any field once every field is valid (huh's
// goes to the next field and submits on the last), tab/↓ and shift+tab/↑ wrap around and
// leaving a field doesn't validate it (huh's stops there), and an action field runs the
// screen's action on enter.
package huhform

import (
	gocolor "image/color"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/luevano/mangal/v5/theme/huhtheme"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/util"
)

// Form is a few fields, enter submits them once they're valid.
type Form struct {
	Fields  []*Field
	form    *huh.Form
	current int
	accent  gocolor.Color

	next, prev, flip, submit, pick key.Binding
}

func New(fields ...*Field) *Form {
	f := &Form{
		Fields: fields,
		next:   util.BindNamedKey("tab", "next", "tab", "down"),
		prev:   util.Bind("previous", "shift+tab", "up"),
		flip:   base.KeyOnOff,
		submit: util.Bind("save", "enter"),
		pick:   key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←/→", "pick")),
	}
	width := 0
	for _, fl := range fields {
		width = max(width, lipgloss.Width(fl.label))
	}
	huhFields := make([]huh.Field, len(fields))
	for i, fl := range fields {
		title := ""
		if fl.label != "" {
			title = fl.label + strings.Repeat(" ", width-lipgloss.Width(fl.label)) + "  "
		}
		fl.setTitle(title)
		huhFields[i] = fl
	}
	// the theme reads the accent on every render, SetAccent changes it
	theme := huh.ThemeFunc(func(bool) *huh.Styles { return huhtheme.Styles(f.accent) })
	f.form = huh.NewForm(huh.NewGroup(huhFields...)).
		WithShowHelp(false).
		WithShowErrors(false).
		WithKeyMap(keyMap()).
		WithTheme(theme)
	fields[0].Focus()
	f.updateKeys()
	return f
}

// keyMap is huh's keymap: Update moves between the fields and submits, so huh's next,
// previous and submit keys are off (as its quit key: esc and ctrl+c are base's); a toggle
// flips with base.KeyOnOff, a choice with ← and →.
func keyMap() *huh.KeyMap {
	off := key.NewBinding(key.WithDisabled())
	km := huh.NewDefaultKeyMap()
	km.Quit = off
	km.Input = huh.InputKeyMap{AcceptSuggestion: off, Next: off, Prev: off, Submit: off}
	km.Confirm = huh.ConfirmKeyMap{Next: off, Prev: off, Submit: off, Toggle: base.KeyOnOff, Accept: off, Reject: off}
	// a choice cycles with the arrows only (h and l page elsewhere)
	km.Select = huh.SelectKeyMap{Next: off, Prev: off, Up: off, Down: off, HalfPageUp: off, HalfPageDown: off, GotoTop: off, GotoBottom: off,
		Left: key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "previous")), Right: key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "next")),
		Filter: off, SetFilter: off, ClearFilter: off, Submit: off}
	return km
}

// SetSubmit sets the help of enter, "save" by default.
func (f *Form) SetSubmit(help string) { f.submit.SetHelp("enter", help) }

// SetAccent sets the color of the labels and the cursor.
func (f *Form) SetAccent(c gocolor.Color) { f.accent = c }

// Focused is the focused field.
func (f *Form) Focused() *Field { return f.Fields[f.current] }

// Current is the index of the focused field.
func (f *Form) Current() int { return f.current }

// Keys are the help of the focused field.
func (f *Form) Keys() []key.Binding {
	var keys []key.Binding
	if a := f.Focused().action; a != nil {
		keys = append(keys, a.binding)
	}
	keys = append(keys, f.submit)
	if len(f.Fields) > 1 {
		keys = append(keys, f.next, f.prev)
	}
	return append(keys, f.flip, f.pick)
}

// Focus focuses the field i (wrapping around), through huh's next and previous field.
func (f *Form) Focus(i int) tea.Cmd {
	i = (i + len(f.Fields)) % len(f.Fields)
	var cmds []tea.Cmd
	// never past the last field: huh would submit
	for ; f.current < i; f.current++ {
		cmds = append(cmds, f.form.NextField())
	}
	for ; f.current > i; f.current-- {
		cmds = append(cmds, f.form.PrevField())
	}
	// again for the cursor's blink
	cmds = append(cmds, f.Fields[i].Focus())
	f.updateKeys()
	return tea.Batch(cmds...)
}

func (f *Form) updateKeys() {
	cur := f.Fields[f.current]
	f.flip.SetEnabled(cur.kind == kindToggle)
	f.pick.SetEnabled(cur.kind == kindChoice)
	f.submit.SetEnabled(cur.kind != kindAction)
}

// Valid validates every field, false focuses the first invalid one.
func (f *Form) Valid() bool {
	first := -1
	for i, fl := range f.Fields {
		if !fl.valid() && first < 0 {
			first = i
		}
	}
	if first >= 0 {
		f.Focus(first)
		return false
	}
	return true
}

// Update handles a msg, submitted is true on enter with valid fields. Enter on an action
// field runs it.
func (f *Form) Update(msg tea.Msg) (submitted bool, cmd tea.Cmd) {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		cur := f.Focused()
		switch {
		case cur.action != nil && key.Matches(msg, cur.action.binding):
			if cur.action.press == nil {
				return false, nil
			}
			return false, cur.action.press()
		case key.Matches(msg, f.submit):
			return f.Valid(), nil
		case key.Matches(msg, f.next):
			return false, f.Focus(f.current + 1)
		case key.Matches(msg, f.prev):
			return false, f.Focus(f.current - 1)
		}
	}
	_, cmd = f.form.Update(msg)
	return false, cmd
}

// View is the fields, one under the other: huh's group keeps a viewport whose content and
// height only change on its Update, the fields change in between (hints, errors, values).
func (f *Form) View() string {
	lines := make([]string, len(f.Fields))
	for i, fl := range f.Fields {
		lines[i] = fl.View()
	}
	return strings.Join(lines, "\n")
}

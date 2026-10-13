// Package form is the TUI's forms: a few fields, the focused one takes the typing, tab
// (or down) and shift+tab (or up) move between them, enter submits.
package form

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/util"
)

// Field is a text input, or an on/off toggle.
type Field struct {
	Label string
	Hint  string
	Input textinput.Model
	// Toggle fields are On or off, space flips them
	Toggle bool
	On     bool
	// Action fields show Display, enter on them is the screen's (not a submit)
	Action  bool
	Display string
	// Validate checks the text, nil accepts anything
	Validate func(string) error
	Err      string
}

func Text(label, value, hint string, validate func(string) error) *Field {
	in := textinput.New()
	in.Prompt = ""
	in.Width = 30
	in.SetValue(value)
	return &Field{Label: label, Hint: hint, Input: in, Validate: validate}
}

// Secret is a Text whose value isn't shown.
func Secret(label, value, hint string, validate func(string) error) *Field {
	f := Text(label, value, hint, validate)
	f.Input.EchoMode = textinput.EchoPassword
	return f
}

func Toggle(label string, on bool, hint string) *Field {
	return &Field{Label: label, Hint: hint, Toggle: true, On: on}
}

func Action(label, display, hint string) *Field {
	return &Field{Label: label, Hint: hint, Action: true, Display: display}
}

// Typed is false for the fields without a text input.
func (f *Field) Typed() bool { return !f.Toggle && !f.Action }

// Value is the text, trimmed.
func (f *Field) Value() string {
	return strings.TrimSpace(f.Input.Value())
}

// Form is a few fields, enter submits them once they're valid.
type Form struct {
	Fields  []*Field
	current int

	next, prev, flip, submit key.Binding
	label                    lipgloss.Style
}

func New(fields ...*Field) *Form {
	f := &Form{
		Fields: fields,
		next:   util.BindNamedKey("tab", "next", "tab", "down"),
		prev:   util.Bind("previous", "shift+tab", "up"),
		flip:   base.KeyOnOff,
		submit: util.Bind("save", "enter"),
		label:  style.Bold.Base.Foreground(color.Accent),
	}
	f.Focus(0)
	return f
}

// SetSubmit sets the help of enter, "save" by default.
func (f *Form) SetSubmit(help string) { f.submit.SetHelp("enter", help) }

// SetAccent sets the color of the labels.
func (f *Form) SetAccent(c lipgloss.Color) { f.label = f.label.Foreground(c) }

// Focused is the focused field.
func (f *Form) Focused() *Field { return f.Fields[f.current] }

// Current is the index of the focused field.
func (f *Form) Current() int { return f.current }

func (f *Form) Keys() []key.Binding {
	return []key.Binding{f.submit, f.next, f.prev, f.flip}
}

func (f *Form) Focus(i int) tea.Cmd {
	f.current = (i + len(f.Fields)) % len(f.Fields)
	var cmd tea.Cmd
	for j, fl := range f.Fields {
		if !fl.Typed() {
			continue
		}
		if j == f.current {
			cmd = fl.Input.Focus()
		} else {
			fl.Input.Blur()
		}
	}
	f.flip.SetEnabled(f.Fields[f.current].Toggle)
	f.submit.SetEnabled(!f.Fields[f.current].Action)
	return cmd
}

// Valid validates every field, false focuses the first invalid one.
func (f *Form) Valid() bool {
	first := -1
	for i, fl := range f.Fields {
		fl.Err = ""
		if !fl.Typed() || fl.Validate == nil {
			continue
		}
		if err := fl.Validate(fl.Value()); err != nil {
			fl.Err = err.Error()
			if first < 0 {
				first = i
			}
		}
	}
	if first >= 0 {
		f.Focus(first)
		return false
	}
	return true
}

// Update handles a msg, submitted is true on enter with valid fields.
func (f *Form) Update(msg tea.Msg) (submitted bool, cmd tea.Cmd) {
	cur := f.Fields[f.current]
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(msg, f.submit):
			return f.Valid(), nil
		case key.Matches(msg, f.next):
			return false, f.Focus(f.current + 1)
		case key.Matches(msg, f.prev):
			return false, f.Focus(f.current - 1)
		case cur.Toggle && key.Matches(msg, f.flip):
			cur.On = !cur.On
			return false, nil
		}
	}
	if !cur.Typed() {
		return false, nil
	}
	cur.Input, cmd = cur.Input.Update(msg)
	return false, cmd
}

var (
	hintStyle = style.Normal.Secondary
	errStyle  = lipgloss.NewStyle().Foreground(color.Error)
)

func (f *Form) View() string {
	width := 0
	for _, fl := range f.Fields {
		width = max(width, lipgloss.Width(fl.Label))
	}
	var lines []string
	for i, fl := range f.Fields {
		cursor := "  "
		if i == f.current {
			cursor = f.label.Render(icon.Item.Raw()) + " "
		}
		value := fl.Input.View()
		if fl.Action {
			value = fl.Display
		}
		if fl.Toggle {
			value = "[ ] off"
			if fl.On {
				value = "[x] on"
			}
		}
		line := cursor + f.label.Render(fl.Label+strings.Repeat(" ", width-lipgloss.Width(fl.Label))) + "  " + value
		lines = append(lines, line)
		if fl.Hint != "" {
			lines = append(lines, "    "+hintStyle.Render(fl.Hint))
		}
		if fl.Err != "" {
			lines = append(lines, "    "+errStyle.Render(fl.Err))
		}
	}
	return strings.Join(lines, "\n")
}

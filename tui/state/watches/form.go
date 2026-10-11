package watches

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/luevano/mangal/v5/tui/util"
)

// field is a text input, or an on/off toggle.
type field struct {
	label string
	hint  string
	input textinput.Model
	// toggle fields are on or off, space flips them
	toggle bool
	on     bool
	// action fields show display, enter on them is the screen's (not a save)
	action  bool
	display string
	// validate checks the text, nil accepts anything
	validate func(string) error
	err      string
}

func textField(label, value, hint string, validate func(string) error) *field {
	in := textinput.New()
	in.Prompt = ""
	in.Width = 30
	in.SetValue(value)
	return &field{label: label, hint: hint, input: in, validate: validate}
}

func toggleField(label string, on bool, hint string) *field {
	return &field{label: label, hint: hint, toggle: true, on: on}
}

func actionField(label, display, hint string) *field {
	return &field{label: label, hint: hint, action: true, display: display}
}

// typed is false for the fields without a text input.
func (f *field) typed() bool { return !f.toggle && !f.action }

func (f *field) value() string {
	return strings.TrimSpace(f.input.Value())
}

// form is a few fields, enter submits them once they're valid.
type form struct {
	fields  []*field
	current int

	next, prev, flip, submit key.Binding
}

func newForm(fields ...*field) *form {
	f := &form{
		fields: fields,
		next:   util.Bind("next", "tab", "down"),
		prev:   util.Bind("previous", "shift+tab", "up"),
		flip:   util.Bind("on/off", " "),
		submit: util.Bind("save", "enter"),
	}
	f.focus(0)
	return f
}

// focused is the focused field.
func (f *form) focused() *field { return f.fields[f.current] }

func (f *form) keys() []key.Binding {
	return []key.Binding{f.submit, f.next, f.prev, f.flip}
}

func (f *form) focus(i int) tea.Cmd {
	f.current = (i + len(f.fields)) % len(f.fields)
	var cmd tea.Cmd
	for j, fl := range f.fields {
		if !fl.typed() {
			continue
		}
		if j == f.current {
			cmd = fl.input.Focus()
		} else {
			fl.input.Blur()
		}
	}
	f.flip.SetEnabled(f.fields[f.current].toggle)
	f.submit.SetEnabled(!f.fields[f.current].action)
	return cmd
}

// valid validates every field, false focuses the first invalid one.
func (f *form) valid() bool {
	first := -1
	for i, fl := range f.fields {
		fl.err = ""
		if !fl.typed() || fl.validate == nil {
			continue
		}
		if err := fl.validate(fl.value()); err != nil {
			fl.err = err.Error()
			if first < 0 {
				first = i
			}
		}
	}
	if first >= 0 {
		f.focus(first)
		return false
	}
	return true
}

// Update handles a msg, submitted is true on enter with valid fields.
func (f *form) Update(msg tea.Msg) (submitted bool, cmd tea.Cmd) {
	cur := f.fields[f.current]
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(msg, f.submit):
			return f.valid(), nil
		case key.Matches(msg, f.next):
			return false, f.focus(f.current + 1)
		case key.Matches(msg, f.prev):
			return false, f.focus(f.current - 1)
		case cur.toggle && key.Matches(msg, f.flip):
			cur.on = !cur.on
			return false, nil
		}
	}
	if !cur.typed() {
		return false, nil
	}
	cur.input, cmd = cur.input.Update(msg)
	return false, cmd
}

var (
	labelStyle = style.Bold.Base.Foreground(color.Accent)
	hintStyle  = style.Normal.Secondary
	errStyle   = lipgloss.NewStyle().Foreground(color.Error)
)

func (f *form) View() string {
	width := 0
	for _, fl := range f.fields {
		width = max(width, lipgloss.Width(fl.label))
	}
	var lines []string
	for i, fl := range f.fields {
		cursor := "  "
		if i == f.current {
			cursor = labelStyle.Render(icon.Item.Raw()) + " "
		}
		value := fl.input.View()
		if fl.action {
			value = fl.display
		}
		if fl.toggle {
			value = "[ ] off"
			if fl.on {
				value = "[x] on"
			}
		}
		line := cursor + labelStyle.Render(fl.label+strings.Repeat(" ", width-lipgloss.Width(fl.label))) + "  " + value
		lines = append(lines, line)
		if fl.hint != "" {
			lines = append(lines, "    "+hintStyle.Render(fl.hint))
		}
		if fl.err != "" {
			lines = append(lines, "    "+errStyle.Render(fl.err))
		}
	}
	return strings.Join(lines, "\n")
}

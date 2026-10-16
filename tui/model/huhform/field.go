package huhform

import (
	"errors"
	"io"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/luevano/mangal/v5/theme/style"
)

type kind int

const (
	kindText kind = iota
	kindToggle
	kindAction
)

// defaultWidth is a text field's width.
const defaultWidth = 30

// Field is a huh field (huh.Input, huh.Confirm or an action) with a label column, a hint
// and its error under it.
type Field struct {
	huh.Field

	kind     kind
	label    string
	hint     string
	validate func(string) error
	err      string
	focused  bool
	title    string
	theme    huh.Theme

	input  *huh.Input
	value  huh.EmbeddedAccessor[string]
	width  int
	secret bool

	confirm *huh.Confirm
	on      bool

	action *action
}

// Text is a text field, validate (nil accepts anything) runs on the trimmed value.
func Text(label, value, hint string, validate func(string) error) *Field {
	f := &Field{kind: kindText, label: label, hint: hint, validate: validate, width: defaultWidth}
	f.input = huh.NewInput().Inline(true).Prompt("")
	f.input.Accessor(&f.value)
	f.Field = f.input
	f.SetValue(value)
	return f
}

// Secret is a Text whose value isn't shown.
func Secret(label, value, hint string, validate func(string) error) *Field {
	f := Text(label, value, hint, validate)
	f.SetSecret(true)
	return f
}

// Secret is true when the text isn't shown.
func (f *Field) Secret() bool { return f.secret }

// Toggle is an on/off field (a huh.Confirm), base.KeyOnOff flips it.
func Toggle(label string, on bool, hint string) *Field {
	f := &Field{kind: kindToggle, label: label, hint: hint, on: on}
	f.confirm = huh.NewConfirm().Inline(true).Affirmative("on").Negative("off").
		WithButtonAlignment(lipgloss.Left).Value(&f.on)
	f.Field = f.confirm
	return f
}

// Action shows display, its key (enter) runs press instead of submitting the form.
func Action(label, display, hint string, binding key.Binding, press func() tea.Cmd) *Field {
	f := &Field{kind: kindAction, label: label, hint: hint}
	f.action = &action{display: display, binding: binding, press: press}
	f.Field = f.action
	return f
}

// Value is the text, trimmed.
func (f *Field) Value() string { return strings.TrimSpace(f.Raw()) }

// Raw is the text as typed.
func (f *Field) Raw() string {
	if f.input == nil {
		return ""
	}
	return f.value.Get()
}

// SetValue sets the text, the cursor at its end.
func (f *Field) SetValue(v string) {
	// the textinput keeps its cursor unless it was empty
	f.value.Set("")
	f.input.Accessor(&f.value)
	f.value.Set(v)
	f.input.Accessor(&f.value)
}

func (f *Field) SetPlaceholder(s string) { f.input.Placeholder(s) }

// SetWidth sets the width of the text.
func (f *Field) SetWidth(w int) {
	f.width = w
	f.fit()
}

// SetSecret hides the text, or shows it.
func (f *Field) SetSecret(secret bool) {
	f.secret = secret
	mode := huh.EchoModeNormal
	if secret {
		mode = huh.EchoModePassword
	}
	f.input.EchoMode(mode)
}

func (f *Field) SetValidate(validate func(string) error) { f.validate = validate }
func (f *Field) SetHint(hint string)                     { f.hint = hint }

// Err is the error of the last validation, until the field is typed in.
func (f *Field) Err() string { return f.err }
func (f *Field) ClearErr()   { f.err = "" }

func (f *Field) On() bool      { return f.on }
func (f *Field) SetOn(on bool) { f.on = on }
func (f *Field) Display() string {
	if f.action == nil {
		return ""
	}
	return f.action.display
}
func (f *Field) SetDisplay(s string) { f.action.display = s }

// Typed is false for the fields without a text input.
func (f *Field) Typed() bool { return f.kind == kindText }

// valid validates the text, setting the error.
func (f *Field) valid() bool {
	f.err = ""
	if f.kind != kindText || f.validate == nil {
		return true
	}
	if err := f.validate(f.Value()); err != nil {
		f.err = err.Error()
		return false
	}
	return true
}

// setTitle sets the label column, title is the padded label.
func (f *Field) setTitle(title string) {
	f.title = title
	switch f.kind {
	case kindText:
		f.input.Title(title)
		f.fit()
	case kindToggle:
		f.confirm.Title(title)
	case kindAction:
		f.action.title = title
	}
}

// fit sizes the textinput to the width: huh.Input.WithWidth takes the title (inline) and
// a column for the cursor off.
func (f *Field) fit() {
	if f.input != nil {
		f.input.WithWidth(lipgloss.Width(f.title) + f.width + 1)
	}
}

// WithTheme implements huh.Field.
func (f *Field) WithTheme(t huh.Theme) huh.Field {
	f.theme = t
	f.Field.WithTheme(t)
	f.fit()
	return f
}

// Focus implements huh.Field.
func (f *Field) Focus() tea.Cmd {
	f.focused = true
	return f.Field.Focus()
}

// Blur implements huh.Field.
func (f *Field) Blur() tea.Cmd {
	f.focused = false
	return f.Field.Blur()
}

// Error implements huh.Field: none, the form validates on submit (huh's would also stop
// moving between the fields).
func (f *Field) Error() error { return nil }

// Update implements huh.Field.
func (f *Field) Update(msg tea.Msg) (huh.Model, tea.Cmd) {
	if _, ok := msg.(tea.KeyPressMsg); ok && f.focused {
		f.err = ""
	}
	m, cmd := f.Field.Update(msg)
	f.Field = m.(huh.Field)
	return f, cmd
}

var hintStyle = style.Normal.Secondary

// View implements huh.Field: the field after the cursor (the theme's select cursor), its
// hint and error under it.
func (f *Field) View() string {
	cursor := "  "
	if f.focused && f.theme != nil {
		cursor = f.theme.Theme(true).Focused.SelectSelector.String()
	}
	lines := []string{cursor + f.Field.View()}
	if f.hint != "" {
		lines = append(lines, "    "+hintStyle.Render(f.hint))
	}
	if f.err != "" {
		lines = append(lines, "    "+style.Normal.Error.Render(f.err))
	}
	return strings.Join(lines, "\n")
}

var _ huh.Field = (*action)(nil)

// action is a field that shows a value, its key runs press (Form.Update).
type action struct {
	title, display string
	binding        key.Binding
	press          func() tea.Cmd

	focused bool
	theme   huh.Theme
	width   int
}

func (a *action) Init() tea.Cmd                            { return nil }
func (a *action) Update(tea.Msg) (huh.Model, tea.Cmd)      { return a, nil }
func (a *action) Focus() tea.Cmd                           { a.focused = true; return nil }
func (a *action) Blur() tea.Cmd                            { a.focused = false; return nil }
func (a *action) Error() error                             { return nil }
func (a *action) Run() error                               { return huh.Run(a) }
func (a *action) Skip() bool                               { return false }
func (a *action) Zoom() bool                               { return false }
func (a *action) KeyBinds() []key.Binding                  { return []key.Binding{a.binding} }
func (a *action) WithKeyMap(*huh.KeyMap) huh.Field         { return a }
func (a *action) WithWidth(w int) huh.Field                { a.width = w; return a }
func (a *action) WithHeight(int) huh.Field                 { return a }
func (a *action) WithPosition(huh.FieldPosition) huh.Field { return a }
func (a *action) GetKey() string                           { return "" }
func (a *action) GetValue() any                            { return a.display }
func (a *action) RunAccessible(io.Writer, io.Reader) error {
	return errors.New("an action needs the TUI")
}

func (a *action) WithTheme(t huh.Theme) huh.Field {
	if a.theme == nil {
		a.theme = t
	}
	return a
}

func (a *action) View() string {
	title := lipgloss.NewStyle().Bold(true)
	if a.theme != nil {
		title = a.theme.Theme(true).Focused.Title
	}
	return title.Render(a.title) + a.display
}

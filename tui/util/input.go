package util

import (
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/luevano/mangal/v5/theme/color"
)

// NewInput is a textinput with InputStyles.
func NewInput() textinput.Model {
	in := textinput.New()
	in.SetStyles(InputStyles())
	return in
}

// InputStyles are bubbles' textinput styles with the prompt, text and cursor in the
// terminal's colors, as they were before bubbles v2 (its prompt is gray).
func InputStyles() textinput.Styles {
	s := textinput.DefaultStyles(color.IsDark())
	s.Focused.Prompt = lipgloss.NewStyle()
	s.Blurred.Prompt = lipgloss.NewStyle()
	s.Blurred.Text = lipgloss.NewStyle()
	s.Cursor.Color = lipgloss.NoColor{}
	return s
}

// Package huhtheme is huh's theme (charm.land/huh/v2) in mangal's colors, for the TUI
// forms, the CLI prompts and the script picker.
package huhtheme

import (
	gocolor "image/color"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/theme/style"
)

// New is the theme with accent for the titles (color.Accent when nil).
func New(accent gocolor.Color) huh.Theme {
	return huh.ThemeFunc(func(bool) *huh.Styles { return Styles(accent) })
}

// Styles are the theme's styles. huh's isDark isn't used: the LightDark colors follow
// color.SetDark, which the TUI and the CLI set.
func Styles(accent gocolor.Color) *huh.Styles {
	if accent == nil {
		accent = color.Accent
	}
	t := huh.ThemeBase(color.IsDark())
	title := style.Bold.Base.Foreground(accent)
	secondary := style.Normal.Secondary
	button := lipgloss.NewStyle().PaddingRight(1)

	f := &t.Focused
	f.Base = lipgloss.NewStyle()
	f.Card = f.Base
	f.Title = title
	f.NoteTitle = title
	f.Description = secondary
	f.ErrorIndicator = style.Normal.Error.SetString(" *")
	f.ErrorMessage = style.Normal.Error
	f.SelectSelector = title.SetString(icon.Item.Raw() + " ")
	f.MultiSelectSelector = f.SelectSelector
	f.NextIndicator = title.MarginLeft(1).SetString("→")
	f.PrevIndicator = title.MarginRight(1).SetString("←")
	f.Option = lipgloss.NewStyle()
	f.SelectedOption = title
	f.UnselectedOption = lipgloss.NewStyle()
	f.Directory = title
	f.Next = button.Foreground(color.Bright).Background(accent)
	// the confirm's (and the forms' toggles') choice shows without colors too
	f.FocusedButton = f.Next.SetString("●")
	f.BlurredButton = button.Foreground(color.Secondary).SetString("○")
	// the terminal's colors, as bubbles' textinput had them before v2 (tui/util.InputStyles)
	f.TextInput.Cursor = lipgloss.NewStyle()
	f.TextInput.Prompt = title
	f.TextInput.Text = lipgloss.NewStyle()
	f.TextInput.Placeholder = secondary

	t.Blurred = t.Focused
	t.Blurred.NextIndicator = lipgloss.NewStyle()
	t.Blurred.PrevIndicator = lipgloss.NewStyle()
	t.Blurred.SelectSelector = lipgloss.NewStyle().SetString("  ")
	t.Blurred.MultiSelectSelector = t.Blurred.SelectSelector

	t.Group.Title = title
	t.Group.Description = secondary
	t.FieldSeparator = lipgloss.NewStyle().SetString("\n")
	t.Help.ShortKey = secondary
	t.Help.ShortDesc = secondary
	t.Help.ShortSeparator = secondary
	return t
}

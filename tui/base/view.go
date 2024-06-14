package base

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View implements tea.Model.
func (m *model) View() string {
	header := m.viewHeader()
	state := m.viewState()
	footer := m.viewFooter()

	return lipgloss.JoinVertical(lipgloss.Left, header, state, footer)
}

func (m *model) viewHeader() string {
	var header strings.Builder
	header.Grow(200)

	title := m.state.Title()
	titleStyle := m.styles.title

	if title.Background != "" {
		titleStyle = titleStyle.Background(title.Background)
	}
	if title.Foreground != "" {
		titleStyle = titleStyle.Foreground(title.Foreground)
	}
	header.WriteString(titleStyle.MaxWidth(m.size.Width / 2).Render(title.Text))

	if status := m.state.Status(); status != "" {
		// TODO: apply a style
		header.WriteString(" ")
		header.WriteString(status)
	}

	if subtitle := m.state.Subtitle(); subtitle != "" {
		header.WriteString("\n\n")
		header.WriteString(m.styles.subtitle.Render(subtitle))
	}

	return m.styles.header.Render(header.String())
}

func (m *model) viewState() string {
	size := m.stateSize()
	style := lipgloss.
		NewStyle().
		MaxWidth(size.Width).
		MaxHeight(size.Height)

	return lipgloss.Place(
		size.Width,
		size.Height,
		lipgloss.Left,
		lipgloss.Top,
		style.Render(m.state.View()),
	)
}

func (m *model) viewFooter() string {
	return m.styles.footer.Render(m.help.View(m.keyMap.with(m.state.KeyMap())))
}

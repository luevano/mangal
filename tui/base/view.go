package base

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/tui/util"

	"charm.land/lipgloss/v2"
	"github.com/luevano/mangal/v5/util/sanitize"
)

const newline = "\n"

// View implements tea.Model: the full TUI is on the alt screen with the mouse wheel
// (viewports scroll), a standalone screen inline without the mouse.
func (m *model) View() tea.View {
	var v tea.View
	if !m.standalone {
		v.AltScreen = true
		v.MouseMode = tea.MouseModeCellMotion
	}
	// clears the inline standalone view
	if m.quitting && m.standalone {
		return v
	}
	header := m.viewHeader()
	state := m.viewState()
	footer := m.viewFooter()

	// provider titles, groups and errors are everywhere in it: only the styles' sequences
	// reach the terminal
	v.SetContent(sanitize.Styled(lipgloss.JoinVertical(lipgloss.Left, header, state, footer)))
	return v
}

func (m *model) viewBreadcrumb() string {
	var bc strings.Builder
	bc.Grow(100)
	sep := m.styles.breadcrumb.sepStyle.Render(m.styles.breadcrumb.sep)
	for _, s := range m.history.states {
		bc.WriteString(m.styles.breadcrumb.state.Render(s.Title().Text))
		bc.WriteString(sep)
	}
	if n := len(m.forward); n > 0 {
		bc.WriteString(m.styles.breadcrumb.state.Render("(" + strconv.Itoa(n) + " ahead: " + m.keyMap.windows.Help().Key + ")"))
	}
	return bc.String()
}

func (m *model) viewHeader() string {
	var header strings.Builder
	header.Grow(200)

	if m.showBreadcrumbs() {
		header.WriteString(m.viewBreadcrumb())
		header.WriteString("\n")
	}

	title := m.state.Title()
	if m.inViewport {
		title = Title{
			Text:       m.viewport.Title(),
			Background: m.viewport.Color(),
		}
	}
	titleStyle := m.styles.title

	if title.Background != nil {
		titleStyle = titleStyle.Background(title.Background)
	}
	if title.Foreground != nil {
		titleStyle = titleStyle.Foreground(title.Foreground)
	}
	header.WriteString(titleStyle.MaxWidth(m.size.Width / 2).Render(title.Text))

	status := m.state.Status()
	if m.inViewport {
		status = m.viewport.Status()
	}
	if status != "" {
		header.WriteString(m.styles.status.Render(status))
	}

	if m.notification != "" {
		width := m.size.Width - lipgloss.Width(header.String())
		header.WriteString(m.styles.notification.MaxWidth(width).Render(m.notification))
	}

	if m.inViewport {
		return m.styles.header.PaddingBottom(0).Render(header.String())
	}

	header.WriteString(newline)
	label := m.loadingLabel()
	if label != "" {
		header.WriteString(m.spinner.View())
		header.WriteString(m.styles.loading.message.Render(label))
	}
	if activity := m.activityStatus(); activity != "" {
		if label != "" {
			header.WriteString("  ")
		}
		header.WriteString(m.styles.loading.message.Render(activity))
	}

	header.WriteString(newline)
	if subtitle := m.state.Subtitle(); subtitle != "" {
		header.WriteString(m.styles.subtitle.Render(subtitle))
	}

	return m.styles.header.Render(header.String())
}

func (m *model) viewState() string {
	if m.inViewport {
		return m.styles.state.Render(m.viewport.View())
	}
	if m.standalone {
		return m.styles.state.Render(m.state.View())
	}
	size := m.stateSize()
	view := lipgloss.Place(
		size.Width,
		size.Height,
		lipgloss.Left,
		lipgloss.Top,
		m.styles.state.Render(m.state.View()),
	)
	switch {
	case m.quitAsk != nil:
		return m.viewQuitAsk(view)
	case m.aheadAsk != nil:
		return m.viewAheadAsk(view)
	case m.switcher != nil:
		return m.viewSwitcher(view)
	}
	return view
}

func (m *model) viewFooter() string {
	switch {
	case m.quitAsk != nil:
		return m.styles.footer.Render(m.help.View(util.KeyMap{Short: []key.Binding{quitYes, quitNo}}))
	case m.aheadAsk != nil:
		return m.styles.footer.Render(m.help.View(util.KeyMap{Short: []key.Binding{aheadYes, aheadNo}}))
	case m.switcher != nil:
		return m.styles.footer.Render(m.help.View(m.switcherKeyMap()))
	}
	if m.inViewport {
		return m.styles.footer.Render(m.help.View(CombinedKeyMap(m.viewport.KeyMap, m.keyMap)))
	}
	return m.styles.footer.Render(m.help.View(CombinedKeyMap(m.state.KeyMap(), m.keyMap)))
}

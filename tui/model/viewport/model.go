package viewport

import (
	"image/color"

	"fmt"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/luevano/mangal/v5/tui/util"
)

type Model struct {
	viewport.Model

	title,
	content string
	// render lays the content out for a width, nil wraps content
	render func(width int) string
	color  color.Color

	borderHorizontalSize,
	borderVerticalSize int

	style  lipgloss.Style
	KeyMap KeyMap
}

func (m *Model) Title() string {
	return m.title
}

func (m *Model) Color() color.Color {
	return m.color
}

func (m *Model) SetContent(content string) {
	m.content, m.render = content, nil
	m.Model.SetContent(m.wrap(content))
	m.updateKeybinds()
}

// SetRender sets content that lays itself out for the viewport's width (render(0) is
// what's copied).
func (m *Model) SetRender(render func(width int) string) {
	m.content, m.render = render(0), render
	m.Model.SetContent(m.wrap(m.content))
	m.updateKeybinds()
}

// wrap breaks the long lines (e.g. notices) at the viewport's width.
func (m *Model) wrap(content string) string {
	if m.Width() <= 0 {
		return content
	}
	if m.render != nil {
		content = m.render(m.Width())
	}
	return lipgloss.NewStyle().Width(m.Width()).Render(content)
}

func (m *Model) SetData(title, content string, c color.Color) {
	m.setData(title, c)
	m.SetContent(content)
	m.GotoTop()
}

// SetDataRender is SetData with SetRender.
func (m *Model) SetDataRender(title string, render func(width int) string, c color.Color) {
	m.setData(title, c)
	m.SetRender(render)
	m.GotoTop()
}

func (m *Model) setData(title string, c color.Color) {
	m.title = title
	m.color = c
	m.style = m.style.BorderForeground(c)
}

func (m *Model) Status() string {
	return fmt.Sprintf("%3.f%%", m.ScrollPercent()*100)
}

// Resize fits the viewport, with its border, in size.
func (m *Model) Resize(size util.Size) tea.Cmd {
	m.SetWidth(size.Width - m.borderHorizontalSize)
	m.SetHeight(size.Height - m.borderVerticalSize)
	if m.content != "" {
		m.Model.SetContent(m.wrap(m.content))
		m.updateKeybinds()
	}
	return nil
}

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.KeyMap.Copy):
			return copyCmd(ansi.Strip(m.content))
		case key.Matches(msg, m.KeyMap.GoTop):
			m.GotoTop()
			return nil
		case key.Matches(msg, m.KeyMap.GoBottom):
			m.GotoBottom()
			return nil
		case key.Matches(msg, m.KeyMap.Back):
			return backCmd
		}
	}
	viewport, cmd := m.Model.Update(msg)
	m.Model = viewport
	return cmd
}

func copyCmd(content string) tea.Cmd {
	return func() tea.Msg {
		return NotificationMsg{Message: util.CopyNotice(content, "Copied content to clipboard")}
	}
}

func (m *Model) View() string {
	return m.style.Render(m.Model.View())
}

// updateKeybinds enables/disables keybinds based on the content.
func (m *Model) updateKeybinds() {
	enable := m.content != ""
	m.KeyMap.Copy.SetEnabled(enable)
	m.KeyMap.GoTop.SetEnabled(enable)
	m.KeyMap.GoBottom.SetEnabled(enable)
}

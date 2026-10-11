package viewport

import (
	"fmt"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Model struct {
	viewport.Model

	title,
	content string
	color lipgloss.Color

	borderHorizontalSize,
	borderVerticalSize int

	style  lipgloss.Style
	KeyMap KeyMap
}

func (m *Model) Title() string {
	return m.title
}

func (m *Model) Color() lipgloss.Color {
	return m.color
}

func (m *Model) SetContent(content string) {
	m.content = content
	m.Model.SetContent(m.wrap(content))
	m.updateKeybinds()
}

// wrap breaks the long lines (e.g. notices) at the viewport's width.
func (m *Model) wrap(content string) string {
	if m.Width <= 0 {
		return content
	}
	return lipgloss.NewStyle().Width(m.Width).Render(content)
}

func (m *Model) SetData(title, content string, color lipgloss.Color) {
	m.title = title
	m.color = color
	m.style = m.style.BorderForeground(color)
	m.SetContent(content)
}

func (s *Model) Status() string {
	return fmt.Sprintf("%3.f%%", s.ScrollPercent()*100)
}

func (m *Model) Resize(width, height int) tea.Cmd {
	m.Width = width - m.borderHorizontalSize
	m.Height = height - m.borderVerticalSize
	if m.content != "" {
		m.Model.SetContent(m.wrap(m.content))
		m.updateKeybinds()
	}
	return nil
}

func (m *Model) Init() tea.Cmd {
	return m.Model.Init()
}

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.KeyMap.Copy):
			return tea.Sequence(
				func() tea.Msg {
					// TODO: sanitize copied text (ansi codes)
					return clipboard.WriteAll(m.content)
				},
				notificationCmd("Copied content to clipboard"),
			)
		case key.Matches(msg, m.KeyMap.GoTop):
			m.GotoTop()
		case key.Matches(msg, m.KeyMap.GoBottom):
			m.GotoBottom()
		case key.Matches(msg, m.KeyMap.Back):
			return backCmd
		}
	}
	viewport, cmd := m.Model.Update(msg)
	m.Model = viewport
	return cmd
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

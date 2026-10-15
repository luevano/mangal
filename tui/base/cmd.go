package base

import (
	"image/color"

	"time"

	tea "charm.land/bubbletea/v2"
)

// Back goes back to the previous state if available.
func Back() tea.Msg {
	return BackMsg{
		Steps: 1,
	}
}

// BackToHome goes back to the initial State (home).
func BackToHome() tea.Msg {
	return BackToHomeMsg{}
}

// Notify sends a notification with the default time.Duration.
func Notify(message string) tea.Cmd {
	return func() tea.Msg {
		return NotificationMsg{Message: message}
	}
}

// NotifyWithDuration sends a notification with the given time.Duration.
func NotifyWithDuration(message string, duration time.Duration) tea.Cmd {
	return func() tea.Msg {
		return NotificationWithDurationMsg{
			NotificationMsg: NotificationMsg{
				Message: message,
			},
			Duration: duration,
		}
	}
}

// Viewport shows content in a scrollable box over the State, esc closes it.
func Viewport(title, content string, color color.Color) tea.Cmd {
	return func() tea.Msg {
		return ShowViewportMsg{
			Title:   title,
			Content: content,
			Color:   color,
		}
	}
}

// ViewportRender is Viewport with content laid out for the viewport's width (again on a
// resize).
func ViewportRender(title string, render func(width int) string, color color.Color) tea.Cmd {
	return func() tea.Msg {
		return ShowViewportMsg{Title: title, Render: render, Color: color}
	}
}

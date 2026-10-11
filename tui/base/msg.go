package base

import (
	"time"

	"github.com/charmbracelet/lipgloss"
)

type BackMsg struct {
	Steps int
}

type BackToHomeMsg struct{}

type NotificationMsg struct {
	Message string
}

type NotificationWithDurationMsg struct {
	NotificationMsg
	Duration time.Duration
}

type notificationTimeoutMsg struct {
	id int
}

// RestoredMsg is sent when going back to the state.
type RestoredMsg struct{}

type ShowViewportMsg struct {
	Title,
	Content string
	Color lipgloss.Color
}

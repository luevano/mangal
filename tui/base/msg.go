package base

import (
	"image/color"

	"time"
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

// RestoredMsg is sent when the State is shown again: going back to it, or a push of the
// same window (see Keyed). Request is that pushed State (nil going back), the State may
// take what it asks for from it (e.g. a filter, a manga to select); it's never shown.
type RestoredMsg struct {
	Request State
}

type ShowViewportMsg struct {
	Title,
	Content string
	// Render, if set, lays the content out for the viewport's width instead of Content
	Render func(width int) string
	Color  color.Color
	// ConfigKey fixes the error shown, a key opens the config window on it
	ConfigKey string
}

package base

import (
	"image/color"
	"slices"

	"time"
)

type BackMsg struct {
	Steps int
}

type BackToHomeMsg struct{}

type NotificationMsg struct {
	Message string
	// Warning shows it longer, as a warning in the history
	Warning bool
}

func (n NotificationMsg) level() string {
	if n.Warning {
		return levelWarning
	}
	return levelInfo
}

type NotificationWithDurationMsg struct {
	NotificationMsg
	Duration time.Duration
}

type notificationTimeoutMsg struct {
	id int
}

// DBChangedMsg says what another process (the CLI, a serve, a watch check) changed in the
// data database: its topics (storage.Rev*). The shown State reloads what it shows of them.
type DBChangedMsg struct {
	Topics []string
}

// Has is true when one of the topics changed.
func (m DBChangedMsg) Has(topics ...string) bool {
	return slices.ContainsFunc(m.Topics, func(t string) bool { return slices.Contains(topics, t) })
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

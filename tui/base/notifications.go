package base

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/theme/color"
)

// The readable time of a notification, the same rule as the web UI's (stores/toasts.ts):
// a minimum plus the time to read it (~1000 characters a minute, slow reading), longer
// for a warning or an error, at most notificationMax.
const (
	notificationMin     = 4 * time.Second
	notificationPerChar = 60 * time.Millisecond
	notificationMax     = 30 * time.Second
)

const (
	levelInfo    = "info"
	levelWarning = "warning"
	levelError   = "error"
)

// shortcut: this session's last notificationsKept only, in memory; persist them if
// they're wanted across runs.
const notificationsKept = 200

type notification struct {
	at          time.Time
	level, text string
}

// readableDuration is how long text stays: the rule, or at least asked when longer.
func readableDuration(text, level string, asked time.Duration) time.Duration {
	d := notificationMin + time.Duration(utf8.RuneCountInString(text))*notificationPerChar
	if level != levelInfo {
		d = d * 3 / 2
	}
	return max(min(d, notificationMax), asked)
}

// notify shows message to the right of the title and status, at least for duration (the
// readable time when longer), and keeps it in the notification history. One replaced
// while shown is counted in the header's hint.
func (m *model) notify(message, level string, duration time.Duration) tea.Cmd {
	m.remember(level, message)
	if m.notification != "" {
		m.notificationsHidden++
	}
	m.notification = message
	m.notificationID++
	id := m.notificationID
	return tea.Tick(readableDuration(message, level, duration), func(time.Time) tea.Msg {
		return notificationTimeoutMsg{id: id}
	})
}

func (m *model) remember(level, text string) {
	m.notifications = append(m.notifications, notification{at: time.Now(), level: level, text: text})
	if n := len(m.notifications); n > notificationsKept {
		m.notifications = m.notifications[n-notificationsKept:]
	}
}

// notificationHeader is the notification shown, with how many it replaced before they
// timed out and the key that lists them.
func (m *model) notificationHeader() string {
	if m.notificationsHidden == 0 {
		return m.notification
	}
	return fmt.Sprintf("(+%d, %s) %s", m.notificationsHidden, m.keyMap.notifications.Help().Key, m.notification)
}

// notificationHistory is this session's notifications, newest first.
func (m *model) notificationHistory() string {
	if len(m.notifications) == 0 {
		return "No notifications yet."
	}
	var b strings.Builder
	for i := len(m.notifications) - 1; i >= 0; i-- {
		n := m.notifications[i]
		fmt.Fprintf(&b, "%s  %-7s  %s\n", n.at.Format(time.TimeOnly), n.level, n.text)
	}
	return b.String()
}

func (m *model) showNotifications() tea.Cmd {
	m.notificationsHidden = 0
	return Viewport("Notifications", m.notificationHistory(), color.Viewport)
}

package base

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/luevano/mangal/v5/tui/util"
)

func TestReadableDuration(t *testing.T) {
	short, long := "Saved", strings.Repeat("x", 200)
	for _, c := range []struct {
		text, level string
		asked, want time.Duration
	}{
		{short, levelInfo, 0, notificationMin + 5*notificationPerChar},
		{long, levelInfo, 0, notificationMin + 200*notificationPerChar},
		{long, levelWarning, 0, (notificationMin + 200*notificationPerChar) * 3 / 2},
		{strings.Repeat("x", 2000), levelInfo, 0, notificationMax},
		{short, levelInfo, time.Minute, time.Minute},
		{short, levelInfo, time.Second, notificationMin + 5*notificationPerChar},
	} {
		if got := readableDuration(c.text, c.level, c.asked); got != c.want {
			t.Errorf("%d chars %s asked %s: %s, want %s", len(c.text), c.level, c.asked, got, c.want)
		}
	}
}

func TestNotificationHistory(t *testing.T) {
	m := newTestModel(t, &fakeState{title: "Home"})
	m.Update(NotificationMsg{Message: "first"})
	m.Update(NotificationMsg{Message: "second", Warning: true})
	m.Update(errors.New("broke"))

	// first was replaced while shown: the header says so
	if h := m.notificationHeader(); h != "(+1, alt+l) second" {
		t.Errorf("header %q", h)
	}
	lines := strings.Split(strings.TrimSpace(m.notificationHistory()), "\n")
	if len(lines) != 3 {
		t.Fatalf("history %q", lines)
	}
	for i, want := range []string{"error    broke", "warning  second", "info     first"} {
		if !strings.HasSuffix(lines[i], want) {
			t.Errorf("line %d %q, want …%q", i, lines[i], want)
		}
	}

	m.Update(BackMsg{Steps: 1}) // closes the error viewport
	cmd := press(m, util.KeyPress("alt+l"))
	if cmd == nil {
		t.Fatal("alt+l opened nothing")
	}
	m.Update(cmd())
	if !m.inViewport || !strings.Contains(ansi.Strip(m.View().Content), "Notifications") {
		t.Errorf("alt+l didn't show the notifications")
	}
	if m.notificationsHidden != 0 {
		t.Errorf("hidden %d after opening the history", m.notificationsHidden)
	}
}

func TestNotificationTimeout(t *testing.T) {
	m := newTestModel(t, &fakeState{title: "Home"})
	m.Update(NotificationMsg{Message: "a"})
	m.Update(NotificationMsg{Message: "b"})
	m.Update(notificationTimeoutMsg{id: m.notificationID - 1})
	if m.notification != "b" {
		t.Errorf("an old timeout hid %q", m.notification)
	}
	m.Update(notificationTimeoutMsg{id: m.notificationID})
	if m.notification != "" || m.notificationsHidden != 0 {
		t.Errorf("shown %q, hidden %d after its timeout", m.notification, m.notificationsHidden)
	}
}

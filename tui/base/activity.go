package base

import (
	gocolor "image/color"

	"context"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/util"
)

// Activity is the TUI's background work (downloads and checks, tui/jobs): its line is in
// the header, quitting asks while there's some, and its changes reach the shown State.
type Activity interface {
	// Wait blocks until something changed and returns the msg for the shown State (a
	// Noticer is also shown as a notification), nil once ctx is done.
	Wait(ctx context.Context) tea.Msg
	// Status is the header's line, empty when there's nothing.
	Status() string
	// Running is what quitting cancels, a line each.
	Running() []string
	// Shutdown stops it all, waiting up to timeout.
	Shutdown(timeout time.Duration)
}

// Noticer is an Activity msg with a notification.
type Noticer interface {
	Notice() string
}

// CurrentActivity is the background work of the TUI (set by the tui package), nil for none.
var CurrentActivity Activity

// NewDownloads makes the Downloads screen (set by the tui package), nil disables its key.
var NewDownloads func() State

// NewLogs makes the Logs screen (set by the tui package), nil shows the recent lines.
var NewLogs func() State

// noticeDuration is how long an Activity notice is shown.
const noticeDuration = 4 * time.Second

type activityMsg struct{ msg tea.Msg }

func (m *model) listenActivity() tea.Cmd {
	if m.activity == nil {
		return nil
	}
	a, ctx := m.activity, m.root
	return func() tea.Msg {
		msg := a.Wait(ctx)
		if msg == nil {
			return nil
		}
		return activityMsg{msg: msg}
	}
}

func (m *model) onActivity(msg tea.Msg) tea.Cmd {
	var notice tea.Cmd
	if n, ok := msg.(Noticer); ok && n.Notice() != "" {
		notice = m.notify(n.Notice(), levelInfo, noticeDuration)
	}
	return tea.Batch(m.listenActivity(), notice, m.state.Update(m.ctx, msg))
}

func (m *model) activityStatus() string {
	if m.activity == nil {
		return ""
	}
	return m.activity.Status()
}

// quit quits, unless there's background work: it asks first then, saying what stops.
func (m *model) quit() tea.Cmd {
	if m.activity != nil && m.quitAsk == nil {
		if running := m.activity.Running(); len(running) != 0 {
			m.quitAsk = running
			return nil
		}
	}
	m.quitting = true
	return tea.Quit
}

var (
	quitYes = key.NewBinding(key.WithKeys("y", "ctrl+c"), key.WithHelp("y", "quit"))
	quitNo  = key.NewBinding(key.WithKeys("n", "esc"), key.WithHelp("n/esc", "stay"))
)

func (m *model) answerQuit(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, quitYes):
		m.quitting = true
		return tea.Quit
	case key.Matches(msg, quitNo):
		m.quitAsk = nil
	}
	return nil
}

// maxQuitLines is how many jobs the quit question lists.
const maxQuitLines = 10

func (m *model) viewQuitAsk(behind string) string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Quit?"))
	b.WriteString("\nThese are cancelled (the chapters downloaded stay):\n")
	for i, line := range m.quitAsk {
		if i == maxQuitLines {
			b.WriteString("  … and ")
			b.WriteString(strconv.Itoa(len(m.quitAsk) - maxQuitLines))
			b.WriteString(" more\n")
			break
		}
		b.WriteString("  • ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(quitYes.Help().Key)
	b.WriteString(" quit · ")
	b.WriteString(quitNo.Help().Key)
	b.WriteString(" stay")
	return m.dialog(b.String(), color.Warning, behind)
}

// dialog draws content in a box over behind.
func (m *model) dialog(content string, c gocolor.Color, behind string) string {
	size := m.stateSize()
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(c).Padding(0, 1).
		MaxWidth(max(20, size.Width-2)).Render(content)
	return util.PlaceCenter(size.Width, size.Height, box, behind)
}

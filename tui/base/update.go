package base

import (
	"context"
	"errors"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/model/viewport"
	"github.com/luevano/mangal/v5/util"
)

// Update implements tea.Model.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.updateKeybinds()
	cmd := m.update(msg)
	m.updateKeybinds()
	return m, cmd
}

func (m *model) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.resize(Size{Width: msg.Width, Height: msg.Height})
	case tea.KeyMsg:
		return m.updateKey(msg)
	case tea.MouseMsg:
		if m.inViewport {
			return m.viewport.Update(msg)
		}
	// receiving any of these msgs override the behavior of the keybinds;
	// even if the keybinds are disabled, these messages will work.
	case ShowViewportMsg:
		cmd := m.showViewport(msg.Title, msg.Content, msg.Color)
		m.viewportConfigKey = msg.ConfigKey
		return cmd
	case OpenConfigMsg:
		return m.openConfig(msg.Filter)
	case viewport.NotificationMsg:
		return m.notify(msg.Message, m.notificationDefaultDuration)
	case viewport.BackMsg:
		return m.hideViewport()
	case BackMsg:
		if m.inViewport {
			return m.hideViewport()
		}
		return m.back(msg.Steps)
	case BackToHomeMsg:
		if m.inViewport {
			return m.hideViewport()
		}
		return m.back(m.history.Size())
	case State:
		return m.pushState(msg)
	case spinner.TickMsg:
		// nil if the spinner is not the correct spinner (the spinner.Tick method handles this)
		// or if trying to spin it too fast, in which case don't hold the spinner.TickMsg hostage
		// and let the state handle it if it needs it (else the state spinner will stay static)
		if cmd := m.updateSpinner(msg); cmd != nil {
			return cmd
		}
	case loadStartMsg:
		return m.startLoad(msg)
	case loadDoneMsg:
		m.stopLoad(msg.id)
		if sc := scopeOf(msg.ctx); sc != nil {
			sc.busy.Add(-1)
		}
		if msg.msg == nil {
			return nil
		}
		if msg.ctx.Err() != nil {
			log.Debug("tui: dropped the result of a left state: %T", msg.msg)
			return nil
		}
		return m.update(msg.msg)
	case NotificationMsg:
		return m.notify(msg.Message, m.notificationDefaultDuration)
	case NotificationWithDurationMsg:
		return m.notify(msg.Message, msg.Duration)
	case notificationTimeoutMsg:
		if msg.id == m.notificationID {
			m.notification = ""
		}
		return nil
	case error:
		if errors.Is(msg, context.Canceled) {
			return nil
		}
		log.Error("%s", msg)
		show := ShowViewportMsg{Title: "Error", Content: util.ErrorTree(msg), Color: color.Error, ConfigKey: configKeyOf(msg)}
		return func() tea.Msg { return show }
	}

	// everything else is the State's, even while the viewport is shown
	return m.state.Update(m.ctx, msg)
}

func (m *model) updateKey(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keyMap.quit), key.Matches(msg, m.keyMap.quitQ):
		m.quitting = true
		return tea.Quit
	case key.Matches(msg, m.keyMap.help):
		return m.toggleHelp()
	}

	if m.inViewport {
		if key.Matches(msg, m.keyMap.configFix) {
			return m.openConfig(m.viewportConfigKey)
		}
		return m.viewport.Update(msg)
	}

	switch {
	case key.Matches(msg, m.keyMap.config):
		return m.openConfig("")
	case key.Matches(msg, m.keyMap.back):
		return Back
	case key.Matches(msg, m.keyMap.home):
		return BackToHome
	case key.Matches(msg, m.keyMap.log):
		return Viewport("Logs", log.Lines(), color.Viewport)
	}
	return m.state.Update(m.ctx, msg)
}

// updateKeybinds enables the global keys that apply now.
func (m *model) updateKeybinds() {
	inputting := !m.inViewport && m.state.Inputting()
	hasHistory := m.history.Size() > 0

	m.keyMap.quitQ.SetEnabled(m.standalone && !inputting)
	m.keyMap.back.SetEnabled(!m.inViewport && hasHistory && m.state.Backable())
	m.keyMap.home.SetEnabled(!m.inViewport && !m.standalone && !inputting && hasHistory)
	m.keyMap.help.SetEnabled(!inputting)
	m.keyMap.log.SetEnabled(!m.inViewport)
	_, inConfig := m.state.(ConfigWindow)
	m.keyMap.config.SetEnabled(NewConfigWindow != nil && !m.standalone && !m.inViewport && !inputting && !inConfig)
	m.keyMap.configFix.SetEnabled(NewConfigWindow != nil && !m.standalone && m.inViewport && m.viewportConfigKey != "")
}

// resize the program
func (m *model) resize(size Size) tea.Cmd {
	m.size = size
	m.help.Width = size.Width
	return m.resizeState()
}

// resizeState only resizes the State with its updated size
func (m *model) resizeState() tea.Cmd {
	s := m.stateSize()
	if m.inViewport {
		return m.viewport.Resize(s)
	}
	return m.state.Resize(s)
}

// back to the previous State
func (m *model) back(steps int) tea.Cmd {
	// do not pop the last state
	if m.history.Size() == 0 || steps <= 0 {
		return nil
	}

	for i := 0; i < steps && m.history.Size() > 0; i++ {
		destroy(m.state)
		m.state = m.history.Pop()
	}
	log.Debug("tui: back to %q", m.state.Title().Text)
	m.newCtx()

	return tea.Sequence(
		m.resizeState(),
		m.state.Update(m.ctx, RestoredMsg{}),
	)
}

// pushState initializes a new state and pushes previous one into history
func (m *model) pushState(state State) tea.Cmd {
	log.Debug("tui: show %q", state.Title().Text)
	m.history.Push(m.state)
	m.state = state
	m.newCtx()

	return tea.Sequence(
		m.state.Init(m.ctx),
		m.resizeState(),
	)
}

// toggleHelp show/hide help menu (keybindings)
func (m *model) toggleHelp() tea.Cmd {
	m.help.ShowAll = !m.help.ShowAll
	return m.resizeState()
}

// updateSpinner updates the spinner to the next frame
func (m *model) updateSpinner(msg spinner.TickMsg) tea.Cmd {
	if len(m.loads) == 0 {
		return nil
	}

	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return cmd
}

// startLoad shows the label of a load (a running one keeps its place).
func (m *model) startLoad(load loadStartMsg) tea.Cmd {
	if load.label == "" {
		return nil
	}
	wasIdle := len(m.loads) == 0
	m.stopLoad(load.id)
	m.loads = append(m.loads, load)
	if wasIdle {
		return m.spinner.Tick
	}
	return nil
}

func (m *model) stopLoad(id int64) {
	for i, l := range m.loads {
		if l.id == id {
			m.loads = append(m.loads[:i], m.loads[i+1:]...)
			return
		}
	}
}

func (m *model) loadingLabel() string {
	if len(m.loads) == 0 {
		return ""
	}
	return m.loads[len(m.loads)-1].label
}

// notify will show a message to the right of the title and status.
func (m *model) notify(message string, duration time.Duration) tea.Cmd {
	m.notification = message
	m.notificationID++
	id := m.notificationID
	return tea.Tick(duration, func(time.Time) tea.Msg {
		return notificationTimeoutMsg{id: id}
	})
}

func (m *model) showViewport(title, content string, color lipgloss.Color) tea.Cmd {
	m.inViewport = true
	m.viewport.SetData(title, content, color)
	return m.viewport.Resize(m.stateSize())
}

func (m *model) hideViewport() tea.Cmd {
	m.inViewport = false
	m.viewportConfigKey = ""
	return m.resizeState()
}

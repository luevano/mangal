package base

import (
	"context"
	"errors"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/model/viewport"
	"github.com/luevano/mangal/v5/util"
	stringutil "github.com/luevano/mangal/v5/util/string"
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
	case tea.KeyPressMsg:
		return m.updateKey(msg)
	case tea.BackgroundColorMsg:
		// the colors that differ on a light background (color.LightDark) and the lists'
		color.SetDark(msg.IsDark())
	case tea.MouseMsg:
		if m.inViewport {
			return m.viewport.Update(msg)
		}
	// receiving any of these msgs override the behavior of the keybinds;
	// even if the keybinds are disabled, these messages will work.
	case ShowViewportMsg:
		cmd := m.showViewport(msg)
		m.viewportConfigKey = msg.ConfigKey
		return cmd
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
	case jumpMsg:
		return m.jumpToKey(msg.key)
	case activityMsg:
		return m.onActivity(msg.msg)
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
	case loadLabelMsg:
		for i := range m.loads {
			if m.loads[i].id == msg.id {
				m.loads[i].label = msg.label
			}
		}
		return msg.next
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

func (m *model) updateKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.quitAsk != nil {
		return m.answerQuit(msg)
	}
	if m.aheadAsk != nil {
		return m.answerAhead(msg)
	}
	if m.switcher != nil {
		return m.updateSwitcher(msg)
	}
	switch {
	case key.Matches(msg, m.keyMap.quit), key.Matches(msg, m.keyMap.quitQ):
		return m.quit()
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
		if NewLogs == nil || m.standalone {
			return Viewport("Logs", log.Lines(), color.Viewport)
		}
		return m.pushState(NewLogs())
	case key.Matches(msg, m.keyMap.windows):
		m.openSwitcher()
		return nil
	case key.Matches(msg, m.keyMap.downloads):
		return m.pushState(NewDownloads())
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
	m.keyMap.windows.SetEnabled(!m.standalone && !m.inViewport && !inputting)
	m.keyMap.downloads.SetEnabled(NewDownloads != nil && !m.standalone && !m.inViewport && !inputting)
}

// resize the program
func (m *model) resize(size Size) tea.Cmd {
	m.size = size
	m.help.SetWidth(size.Width)
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

// back closes the shown window and the ones ahead of it, then steps-1 more under it, and
// shows the one under them with a RestoredMsg.
func (m *model) back(steps int) tea.Cmd {
	// do not pop the last state
	if m.history.Size() == 0 || steps <= 0 {
		return nil
	}
	m.dropForward()
	for popped := 0; popped < steps && m.history.Size() > 0; popped++ {
		destroy(m.state)
		m.state = m.history.Pop()
	}
	log.Debug("tui: back to %q", m.state.Title().Text)
	return m.restore(nil)
}

// restore shows the State again with a new ctx and a RestoredMsg of request.
func (m *model) restore(request State) tea.Cmd {
	if request == m.state {
		request = nil
	}
	m.newCtx()
	cmd := tea.Sequence(
		m.resizeState(),
		m.state.Update(m.ctx, RestoredMsg{Request: request}),
	)
	// it was never shown, only read
	destroy(request)
	return cmd
}

// pushState shows state, the shown one goes to the history. The same window (Keyed, or
// the same instance) open anywhere (history, shown, ahead) is shown instead, with state as
// the request: no window is open twice. A new window while there are windows ahead asks
// first (aheadAsk): yes closes them and shows it, no drops it.
func (m *model) pushState(state State) tea.Cmd {
	key := keyOf(state)
	for i, s := range m.windows() {
		if s == state || (key != "" && keyOf(s) == key) {
			return m.jump(i, state)
		}
	}
	if len(m.forward) > 0 {
		if m.aheadAsk != nil && m.aheadAsk != state {
			destroy(m.aheadAsk)
		}
		m.aheadAsk = state
		if m.inViewport {
			return m.hideViewport()
		}
		return nil
	}
	return m.pushNew(state)
}

// pushNew shows state, a new window, the windows ahead are closed.
func (m *model) pushNew(state State) tea.Cmd {
	log.Debug("tui: show %q", state.Title().Text)
	var notice tea.Cmd
	if n := m.dropForward(); n > 0 {
		notice = Notify("Closed " + stringutil.Quantify(n, "window", "windows") + " ahead")
	}
	m.history.Push(m.state)
	m.state = state
	m.newCtx()

	return tea.Sequence(
		m.state.Init(m.ctx),
		m.resizeState(),
		notice,
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

func (m *model) showViewport(msg ShowViewportMsg) tea.Cmd {
	m.inViewport = true
	if msg.Render != nil {
		m.viewport.SetDataRender(msg.Title, msg.Render, msg.Color)
	} else {
		m.viewport.SetData(msg.Title, msg.Content, msg.Color)
	}
	return m.viewport.Resize(m.stateSize())
}

func (m *model) hideViewport() tea.Cmd {
	m.inViewport = false
	m.viewportConfigKey = ""
	return m.resizeState()
}

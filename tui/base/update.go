package base

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/model/viewport"
	"github.com/luevano/mangal/v5/util"
	"github.com/pkg/errors"
)

// Update implements tea.Model.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m, m.resize(Size{Width: msg.Width, Height: msg.Height})
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keyMap.quit):
			return m, tea.Quit
		case key.Matches(msg, m.keyMap.back) && m.state.Backable():
			return m, Back
		case key.Matches(msg, m.keyMap.home):
			return m, BackToHome
		case key.Matches(msg, m.keyMap.help):
			return m, m.toggleHelp()
		case key.Matches(msg, m.keyMap.log):
			return m, Viewport("Logs", log.Aggregate.String(), color.Viewport)
		}
	// receiving any of these msgs override the behavior of the keybinds;
	// even if the keybinds are disabled, these messages will work.
	case ShowViewportMsg:
		return m, m.showViewport(msg.Title, msg.Content, msg.Color)
	case viewport.NotificationMsg:
		return m, Notify(msg.Message)
	case viewport.BackMsg:
		return m, m.hideViewport()
	case BackMsg:
		if m.inViewport {
			return m, m.hideViewport()
		}
		return m, m.back(msg.Steps)
	case BackToHomeMsg:
		if m.inViewport {
			return m, m.hideViewport()
		}
		return m, m.back(m.history.Size())
	case State:
		return m, m.pushState(msg)
	case spinner.TickMsg:
		// nil if the spinner is not the correct spinner (the spinner.Tick method handles this)
		// or if trying to spin it too fast, in which case don't hold the spinner.TickMsg hostage
		// and let the state handle it if it needs it (else the state spinner will stay static)
		if cmd := m.updateSpinner(msg); cmd != nil {
			return m, cmd
		}
		return m, m.state.Update(m.ctx, msg)
	case LoadingMsg:
		return m, m.loading(msg.Message)
	case NotificationMsg:
		return m, m.notify(msg.Message, m.notificationDefaultDuration)
	case NotificationWithDurationMsg:
		return m, m.notify(msg.Message, msg.Duration)
	case NotificationTimeoutMsg:
		return m, m.hideNotification()
	case error:
		if errors.Is(msg, context.Canceled) || strings.Contains(msg.Error(), context.Canceled.Error()) {
			return m, nil
		}
		log.L.Err(msg).Msg("")
		return m, Viewport("Error", util.ErrorTree(msg), color.Error)
	}

	if m.inViewport {
		return m, m.viewport.Update(msg)
	}
	return m, m.state.Update(m.ctx, msg)
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
		return m.viewport.Resize(s.Width, s.Height)
	}
	return m.state.Resize(s)
}

// back to the previous State
func (m *model) back(steps int) tea.Cmd {
	// do not pop the last state
	if m.history.Size() == 0 || steps <= 0 {
		return nil
	}

	log.L.Info().Str("state", m.history.Peek().Title().Text).Int("steps", steps).Msg("going back to previous state")

	m.cancel()
	for i := 0; i < steps && m.history.Size() > 0; i++ {
		// TODO: perform m.state.Destroy() once implemented?
		m.state = m.history.Pop()
	}

	return tea.Sequence(
		m.resizeState(),
		m.state.Update(m.ctx, RestoredMsg{}),
	)
}

// pushState initializes a new state and pushes previous one into history
func (m *model) pushState(state State) tea.Cmd {
	log.L.Info().Str("state", state.Title().Text).Msg("new state")
	if !m.state.Intermediate() {
		m.history.Push(m.state)
	}
	m.state = state

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
	if m.loadingMessage == "" {
		return nil
	}

	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return cmd
}

// loading sets the loading message and sends a spinner.Tick msg
func (m *model) loading(message string) tea.Cmd {
	// this check saves one m.resizeState call
	// when calling loading on the same message,
	// which could happen if Loaded cmd is called consecutively
	if m.loadingMessage == message {
		return nil
	}
	m.loadingMessage = message
	if message == "" {
		return m.resizeState()
	}
	return m.spinner.Tick
}

// notify will show a message to the right of the title and status.
func (m *model) notify(message string, duration time.Duration) tea.Cmd {
	m.notification = message
	return tea.Tick(duration, func(t time.Time) tea.Msg {
		return NotificationTimeoutMsg{}
	})
}

// hideNotification removes the notification
func (m *model) hideNotification() tea.Cmd {
	m.notification = ""
	return nil
}

func (m *model) showViewport(title, content string, color lipgloss.Color) tea.Cmd {
	m.inViewport = true
	m.showLoadingMessage = false
	m.showSubtitle = false
	m.viewport.SetData(title, content, color)
	m.updateKeybinds()
	s := m.stateSize()
	return m.viewport.Resize(s.Width, s.Height)
}

func (m *model) hideViewport() tea.Cmd {
	m.inViewport = false
	m.showLoadingMessage = true
	m.showSubtitle = true
	m.updateKeybinds()
	return nil
}

// updateKeybinds will enable/disable keybinds depending on availability
func (m *model) updateKeybinds() {
	enable := !m.inViewport
	m.keyMap.back.SetEnabled(enable)
	m.keyMap.home.SetEnabled(enable)
	m.keyMap.log.SetEnabled(enable)
}

package anilist

import (
	"errors"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/skratchdot/open-golang/open"
)

// loginCachedCmd logs in with the user's saved login.
func (m *Model) loginCachedCmd(item *item) tea.Cmd {
	return func() tea.Msg {
		user, err := anilist.LoginSaved(m.ctx, item.user)
		var expired *anilist.ExpiredError
		switch {
		case errors.Is(err, anilist.ErrUserNotFound):
			if err := m.updateUserHistory(); err != nil {
				return err
			}
			return NotificationMsg(fmt.Sprintf("cached user %q not found, deleted from history", item.user))
		case errors.As(err, &expired):
			m.inNew = true
			m.updateKeybinds()
			return NotificationMsg(expired.Error())
		case err != nil:
			return err
		}
		m.user = user
		if err := m.updateUserHistory(); err != nil {
			return err
		}

		m.state = LoggedIn
		m.inNew = false
		m.updateKeybinds()
		return NotificationMsg(fmt.Sprintf("cached user %q logged in to anilist", item.user))
	}
}

// TODO: handle automatic oauth (interactive) and
// option to directly provide the code/token
//
// loginCmd will attempt to login into Anilist with the given
// Anilist API credentials.
func (m *Model) loginCmd() tea.Msg {
	user, err := anilist.LoginNew(m.ctx, m.idInput.Value(), m.secretInput.Value())
	if err != nil {
		return NotificationMsg("anilist login error: " + err.Error())
	}
	m.user = user
	if err := m.updateUserHistory(); err != nil {
		return err
	}

	// Update model state
	m.state = LoggedIn
	m.inNew = false
	m.updateKeybinds()
	return NotificationMsg(fmt.Sprintf("user %q logged in to anilist", m.user.String()))
}

// logoutCmd will attempt to logout of Anilist.
func (m *Model) logoutCmd() tea.Msg {
	if err := anilist.Logout(); err != nil {
		return NotificationMsg("anilist logout error: " + err.Error())
	}

	username := m.user.Name()
	m.user = nil

	m.state = LoggedOut
	m.updateKeybinds()
	return NotificationMsg(fmt.Sprintf("user %q logged out of anilist", username))
}

func (m *Model) deleteUserCmd(item *item) tea.Cmd {
	return func() tea.Msg {
		if err := anilist.Forget(item.user); err != nil {
			return err
		}
		if err := m.updateUserHistory(); err != nil {
			return err
		}

		// Update login state
		if m.userHistory.Size() == 0 {
			m.inNew = true
		}
		m.updateKeybinds()
		return NotificationMsg(fmt.Sprintf("deleted user %q from cache", item.user))
	}
}

// updateInputCmd sets the current input command to the one hovered
func (m *Model) updateInputCmd(msg tea.Msg) tea.Cmd {
	input, cmd := m.input.Update(msg)
	switch m.current {
	case ID:
		m.idInput = input
	case Secret:
		m.secretInput = input
	case Code:
		m.codeInput = input
	}
	m.updateCurrent()
	return cmd
}

func (m *Model) openAuthURLCmd() tea.Msg {
	codeType := "code"
	if m.secretInput.Value() == "" {
		codeType = "token"
	}
	err := open.Run(fmt.Sprintf(authURL, m.idInput.Value(), codeType))
	if err != nil {
		return err
	}
	return NotificationMsg("opening auth url")
}

func (m *Model) notifyCmd(message string) tea.Cmd {
	m.notification = message
	return tea.Tick(m.notificationDuration, func(t time.Time) tea.Msg {
		return NotificationTimeoutMsg{}
	})
}

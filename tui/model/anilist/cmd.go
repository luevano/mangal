package anilist

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/auth/oauth"
	"github.com/luevano/mangal/util/cache"
	"github.com/skratchdot/open-golang/open"
	"golang.org/x/oauth2"
)

// loginCachedCmd will attempt to login from a cached user oauth2 token.
func (m *Model) loginCachedCmd(item *item) tea.Cmd {
	return func() tea.Msg {
		// TODO: handle cache error to also delete the username auth history?
		var token oauth2.Token
		found, err := cache.GetAnilistAuthData(item.user, &token)
		if err != nil {
			return err
		}
		if !found {
			m.userHistory.Delete(item.user)
			if err := m.updateUserHistory(); err != nil {
				return err
			}
			return NotificationMsg(fmt.Sprintf("cached user %q not found, deleted from history", item.user))
		}

		// TODO: check if the error is due to expired token
		// delete cached username/token and ask for re-authentication
		err = m.anilist.LoginContext(m.ctx, token.AccessToken)
		if err != nil {
			return err
		}

		// Update auth user
		err = m.updateAuthUser()
		if err != nil {
			return err
		}

		// Update user history
		m.userHistory.Add(m.user.Name())
		err = m.updateUserHistory()
		if err != nil {
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
	id := sanitize(m.idInput.Value())
	secret := sanitize(m.secretInput.Value()) // may be empty (implicit grant)
	if id == "" {
		return NotificationMsg("anilist login error: ID is empty")
	}

	// Get Anilist login option and attempt to authorize
	loginOption, err := oauth.NewAnilistLoginOption(id, secret)
	if err != nil {
		return err
	}
	err = loginOption.Authorize(m.ctx)
	if err != nil {
		return err
	}

	// Get just authorized token and attempt to login
	token := loginOption.Token()
	err = m.anilist.LoginContext(m.ctx, token.AccessToken)
	if err != nil {
		return NotificationMsg("anilist login error: " + err.Error())
	}

	// Update auth user
	err = m.updateAuthUser()
	if err != nil {
		return err
	}

	// Store user's oauth2 Token
	err = cache.SetAnilistAuthData(m.user.Name(), token)
	if err != nil {
		return err
	}

	// Update user history
	m.userHistory.Add(m.user.Name())
	err = m.updateUserHistory()
	if err != nil {
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
	if err := m.anilist.Logout(); err != nil {
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
		// Remove user's oauth2 Token
		err := cache.DeleteAnilistAuthData(item.user)
		if err != nil {
			return err
		}

		// Update user history
		m.userHistory.Delete(item.user)
		err = m.updateUserHistory()
		if err != nil {
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

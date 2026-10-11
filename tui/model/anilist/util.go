package anilist

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/util/cache"
)

func (m *Model) updateCurrent() {
	switch m.current {
	case ID:
		m.input = &m.idInput
	case Secret:
		m.input = &m.secretInput
	case Code:
		m.input = &m.codeInput
	}
}

// updateUserHistory reloads the saved users into the model list.
func (m *Model) updateUserHistory() error {
	users, err := anilist.Users()
	if err != nil {
		return err
	}
	m.userHistory = cache.UserHistory(users)

	items := make([]list.Item, m.userHistory.Size())
	for i, u := range m.userHistory {
		items[i] = &item{user: u}
	}
	m.list.SetItems(items)

	// Expand the list view if needed
	size := m.list.Size()
	size.Height = min(20, len(items))
	m.Resize(size)
	return nil
}

// updateKeybinds enables/disables the keybinds depending on the state of
// the anilist authentication status.
func (m *Model) updateKeybinds() {
	standalone := m.standalone && !m.inInput
	m.keyMap.help.SetEnabled(standalone)
	m.keyMap.quit.SetEnabled(standalone)
	m.keyMap.back.SetEnabled(m.inNew && !m.inInput && m.userHistory.Size() != 0)

	loggable := m.LoggedOut() && !m.inInput
	m.keyMap.login.SetEnabled(loggable)
	m.keyMap.open.SetEnabled(loggable && m.inNew && m.idInput.Value() != "")
	m.keyMap.logout.SetEnabled(m.LoggedIn())
	m.keyMap.new.SetEnabled(m.LoggedOut() && !m.inNew)
	m.keyMap.delete.SetEnabled(m.LoggedOut() && !m.inNew && m.userHistory.Size() != 0)

	navigate := m.inNew && !m.inInput
	m.keyMap.up.SetEnabled(navigate)
	m.keyMap.down.SetEnabled(navigate)
	m.keyMap.selekt.SetEnabled(navigate)
	m.keyMap.clear.SetEnabled(navigate)

	typing := m.inNew && m.inInput
	m.keyMap.confirm.SetEnabled(typing)
	m.keyMap.cancel.SetEnabled(typing)
}

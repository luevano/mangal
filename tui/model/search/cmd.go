package search

import tea "charm.land/bubbletea/v2"

func SearchCmd(query string) tea.Cmd {
	return func() tea.Msg {
		return SearchMsg(query)
	}
}

func SearchCancelCmd() tea.Msg {
	return SearchCancelMsg{}
}

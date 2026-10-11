package format

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/config"
)

func backCmd() tea.Msg {
	return BackMsg{}
}

func (m *Model) setFormatForCmd(what forWhat, format libmangal.Format) tea.Cmd {
	return func() tea.Msg {
		var err error
		switch what {
		case forRead:
			err = config.Read.Format.Set(format)
		case forDownload:
			err = config.Download.Format.Set(format)
		case forBoth:
			err = config.Read.Format.Set(format)
			if err != nil {
				return err
			}
			err = config.Download.Format.Set(format)
		}
		if err != nil {
			return err
		}
		return config.Write()
	}
}

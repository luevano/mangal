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
		if err := config.CreateFile(); err != nil {
			return err
		}
		var err error
		switch what {
		case forRead:
			err = config.Read.Format.Write(format)
		case forDownload:
			err = config.Download.Format.Write(format)
		case forBoth:
			if err = config.Read.Format.Write(format); err == nil {
				err = config.Download.Format.Write(format)
			}
		}
		return err
	}
}

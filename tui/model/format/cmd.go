package format

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/tui/base"
)

func backCmd() tea.Msg {
	return BackMsg{}
}

// setFormat writes the format to the config file. It runs in Update: the items read the
// config to render.
func (m *Model) setFormat(what forWhat, format libmangal.Format) tea.Cmd {
	err := config.CreateFile()
	if err == nil {
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
	}
	if err != nil {
		return func() tea.Msg { return fmt.Errorf("set the %s format: %w", what, err) }
	}
	return base.Notify(fmt.Sprintf("%s format set to %s", what, format))
}

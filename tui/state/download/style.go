package download

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/theme/style"
)

type styles struct {
	sep,
	itemEnum,
	subItemEnum,
	accent,
	warning,
	toDownload,
	succeed,
	failed lipgloss.Style
}

func defaultStyles() styles {
	return styles{
		sep:         style.Bold.Warning.Padding(0, 1),
		itemEnum:    style.Normal.Base.MarginRight(1).PaddingLeft(2),
		subItemEnum: style.Normal.Base.MarginRight(1).PaddingLeft(4),
		accent:      style.Bold.Accent,
		warning:     style.Bold.Warning,
		toDownload:  style.Normal.Secondary,
		succeed:     style.Normal.Success,
		failed:      style.Normal.Error,
	}
}

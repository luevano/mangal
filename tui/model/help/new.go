package help

import (
	"charm.land/bubbles/v2/help"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/theme/style"
)

func New() help.Model {
	h := help.New()
	h.Ellipsis = icon.Ellipsis.Raw()
	h.ShortSeparator = " " + icon.Separator.Raw() + " "
	h.FullSeparator = " " + icon.Separator.Raw() + " "

	keyStyle := style.Bold.Warning
	sepStyle := style.Normal.Base
	descStyle := style.Normal.Secondary

	h.Styles = help.Styles{
		Ellipsis:       style.Normal.Base,
		ShortKey:       keyStyle,
		ShortDesc:      descStyle,
		ShortSeparator: sepStyle,
		FullKey:        keyStyle,
		FullDesc:       descStyle,
		FullSeparator:  sepStyle,
	}
	return h
}

package base

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/style"
)

var DotSpinner = spinner.Spinner{
	Frames: []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"},
	FPS:    time.Second / 10,
}

type styles struct {
	title,
	status,
	notification,
	subtitle,
	header,
	state,
	footer lipgloss.Style

	breadcrumb breadcrumbStyle
	loading    loadingStyle
}

type breadcrumbStyle struct {
	sep string
	sepStyle,
	state lipgloss.Style
}

type loadingStyle struct {
	spinner spinner.Spinner
	spinnerStyle,
	message lipgloss.Style
}

func defaultStyles() styles {
	// flipped Accent
	tempAccent := style.Bold.Accent.Background(color.Background)
	return styles{
		title:        style.FlipGrounds(tempAccent).Padding(0, 1).MarginLeft(1),
		status:       style.Normal.Base.PaddingLeft(1),
		notification: style.Normal.Warning.PaddingLeft(1),
		subtitle:     style.Normal.Secondary.PaddingLeft(2),
		header:       style.Normal.Base.Padding(0, 0, 1, 1),
		state:        style.Normal.Base.Padding(0, 1),
		footer:       style.Normal.Base.Padding(0, 1),
		breadcrumb: breadcrumbStyle{
			sep:      "/",
			sepStyle: style.Normal.Accent.PaddingLeft(1),
			state:    style.Normal.Secondary.PaddingLeft(1),
		},
		loading: loadingStyle{
			spinner:      DotSpinner,
			spinnerStyle: style.Bold.Accent,
			message:      style.Normal.Secondary.PaddingLeft(1),
		},
	}
}

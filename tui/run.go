package tui

import (
	"context"

	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/jobs"
	configwindow "github.com/luevano/mangal/v5/tui/state/config"
	"github.com/luevano/mangal/v5/tui/state/downloads"
	"github.com/luevano/mangal/v5/tui/state/home"
	"github.com/luevano/mangal/v5/util/cache"
)

// Run runs the TUI until the user quits or ctx is cancelled.
func Run(ctx context.Context) error {
	notices := cache.Notices()
	if notice := anilist.LoginNotice(); notice != "" {
		notices = append(notices, notice)
	}
	base.NewConfigWindow = configwindow.New
	base.NewDownloads = downloads.New
	base.CurrentActivity = jobs.Start(ctx)
	return base.Run(ctx, home.New(), notices...)
}

package tui

import (
	"context"

	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/state/home"
	"github.com/luevano/mangal/v5/util/cache"
)

// Run runs the TUI until the user quits or ctx is cancelled.
func Run(ctx context.Context) error {
	notices := cache.Notices()
	if notice := anilist.LoginNotice(); notice != "" {
		notices = append(notices, notice)
	}
	return base.Run(ctx, home.New(), notices...)
}

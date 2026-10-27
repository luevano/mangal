package base

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/util/cache"
)

// shutdownTimeout is how long quitting waits for the background work to stop.
const shutdownTimeout = 3 * time.Second

// Run runs the TUI full screen (the alt screen, mouse wheel: see View) with state as the first (home) State, the notices are
// shown when it starts. It returns when the user quits or ctx is cancelled, which also
// cancels the States' ctx.
func Run(ctx context.Context, state State, notices ...string) error {
	return run(ctx, newModel(ctx, state, false, notices))
}

// RunStandalone runs a single State inline, for the commands that show one screen
// (mangal path, accounts, anilist): no home, breadcrumbs or H, and q quits.
func RunStandalone(ctx context.Context, state State) error {
	return run(ctx, newModel(ctx, state, true, nil))
}

func run(ctx context.Context, m *model) error {
	p := tea.NewProgram(m, tea.WithContext(ctx))
	// other processes' changes reach the shown State (DBChangedMsg)
	if st, err := cache.Storage(); err == nil {
		wctx, stop := context.WithCancel(ctx)
		defer stop()
		go core.NewDBWatcher(st.Data).Run(wctx, func(topics []string) { p.Send(DBChangedMsg{Topics: topics}) })
	}
	_, err := p.Run()
	m.shutdown(shutdownTimeout)
	// killed by ctrl+c from outside (the signal cancels ctx): nothing to report
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		return nil
	}
	return err
}

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

// SwitchMsg asks to switch the TUI to the connection (local, a name, an address).
type SwitchMsg struct{ Value string }

// SwitchTo asks to switch the TUI to the connection: the screens belong to the backend, so it
// ends the program (asking first if that cancels this process' downloads) and the tui package
// starts it again on the other: Run's SwitchError.
func SwitchTo(value string) tea.Cmd {
	return func() tea.Msg { return SwitchMsg{Value: value} }
}

// SwitchError is Run ending to switch to the connection.
type SwitchError struct{ Value string }

func (e *SwitchError) Error() string { return "switch to " + e.Value }

// NewConnections makes the connections' picker (set by the tui package), nil disables its key.
var NewConnections func() State

func run(ctx context.Context, m *model) error {
	p := tea.NewProgram(m, tea.WithContext(ctx))
	// other processes' changes reach the shown State (DBChangedMsg), a remote server's
	// from its events
	wctx, stop := context.WithCancel(ctx)
	defer stop()
	send := func(topics []string) { p.Send(DBChangedMsg{Topics: topics}) }
	if r := CurrentRemote; r != nil {
		if r.Events != nil {
			go r.Events(wctx, send)
		}
	} else if st, err := cache.Storage(); err == nil {
		go core.NewDBWatcher(st.Data).Run(wctx, send)
	}
	_, err := p.Run()
	m.shutdown(shutdownTimeout)
	if m.switchTo != nil && m.quitting {
		return &SwitchError{Value: *m.switchTo}
	}
	// killed by ctrl+c from outside (the signal cancels ctx): nothing to report
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		return nil
	}
	return err
}

package base

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/theme/color"
	mhelp "github.com/luevano/mangal/v5/tui/model/help"
	"github.com/luevano/mangal/v5/tui/model/viewport"
)

var _ tea.Model = (*model)(nil)

// model implements tea.model
//
// model is the parent of all States (windows), could be thought of as the main window.
type model struct {
	// notices are shown when the model starts
	notices []string

	state    State
	viewport *viewport.Model
	history  *history

	// root is cancelled on quit, ctx (the shown State's) when the State changes
	root       context.Context
	rootCancel context.CancelFunc
	ctx        context.Context
	ctxCancel  context.CancelFunc
	work       *tracker

	// standalone runs a single State (and what it pushes) inline, for mangal path etc.
	standalone bool
	quitting   bool

	size Size

	styles  styles
	keyMap  *keyMap
	help    help.Model
	spinner spinner.Model

	// loads are the running loads, the last one is shown
	loads []loadStartMsg

	notification                string
	notificationID              int
	notificationDefaultDuration time.Duration

	inViewport bool
	// viewportConfigKey is the config key of the error shown in the viewport
	viewportConfigKey string
}

func newModel(ctx context.Context, state State, standalone bool, notices []string) *model {
	_styles := defaultStyles()
	_spinner := spinner.New(
		spinner.WithSpinner(_styles.loading.spinner),
		spinner.WithStyle(_styles.loading.spinnerStyle),
	)

	m := &model{
		state:                       state,
		viewport:                    viewport.New(),
		history:                     &history{},
		work:                        &tracker{},
		standalone:                  standalone,
		styles:                      _styles,
		keyMap:                      newKeyMap(),
		help:                        mhelp.New(),
		spinner:                     _spinner,
		notificationDefaultDuration: time.Second,
		notices:                     notices,
	}
	m.root, m.rootCancel = context.WithCancel(ctx)
	m.newCtx()
	m.updateKeybinds()
	return m
}

// showBreadcrumbs is read each time, the config window can change it.
func (m *model) showBreadcrumbs() bool {
	return !m.standalone && config.TUI.ShowBreadcrumbs.Get()
}

// newCtx cancels the ctx of the shown State and makes the next one.
func (m *model) newCtx() {
	if m.ctxCancel != nil {
		m.ctxCancel()
	}
	ctx := context.WithValue(m.root, scopeKey{}, &scope{work: m.work, root: m.root})
	m.ctx, m.ctxCancel = context.WithCancel(ctx)
}

// shutdown cancels everything, tears the States down and waits a moment for the
// background work to stop.
func (m *model) shutdown(timeout time.Duration) {
	m.rootCancel()
	destroy(m.state)
	for m.history.Size() > 0 {
		destroy(m.history.Pop())
	}
	m.work.wait(timeout)
}

// Init implements tea.Model.
func (m *model) Init() tea.Cmd {
	if len(m.notices) == 0 {
		return m.state.Init(m.ctx)
	}
	return tea.Batch(m.state.Init(m.ctx), Viewport("Notice", strings.Join(m.notices, "\n\n"), color.Warning))
}

// stateSize returns the usable size of the state viewport (everything but the header/footer).
func (m *model) stateSize() Size {
	header := m.viewHeader()
	footer := m.viewFooter()
	// state paddings
	top, right, bottom, left := m.styles.state.GetPadding()

	size := m.size
	size.Height -= lipgloss.Height(header) + lipgloss.Height(footer) + top + bottom
	size.Width -= left + right

	return size
}

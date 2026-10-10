package base

import (
	"context"
	"github.com/luevano/mangal/theme/color"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/tui/model/viewport"
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

	ctx       context.Context
	ctxCancel context.CancelFunc

	size Size

	styles  styles
	keyMap  *keyMap
	help    help.Model
	spinner spinner.Model

	loadingMessage string

	notification                string
	notificationDefaultDuration time.Duration

	showBreadcrumbs    bool
	showLoadingMessage bool
	showSubtitle       bool

	inViewport bool
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

func (m *model) cancel() {
	m.ctxCancel()
	m.ctx, m.ctxCancel = context.WithCancel(context.Background())
}

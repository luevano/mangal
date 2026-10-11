package library

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/confirm"
	"github.com/luevano/mangal/v5/tui/util"
)

// confirmer asks before doing something, shown over the state's view.
type confirmer struct {
	model    *confirm.Model
	active   bool
	onYes    tea.Cmd
	behind   string
	size     base.Size
	viewport lipgloss.Style
}

func newConfirmer() confirmer {
	return confirmer{
		model:    confirm.New(40, color.Warning),
		viewport: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(color.Warning).Padding(0, 1),
	}
}

func (c *confirmer) ask(title, message, behind string, onYes tea.Cmd) {
	c.model.SetData(title, message)
	c.active, c.onYes, c.behind = true, onYes, behind
}

// update handles the dialog, done is true if the message was for it.
func (c *confirmer) update(msg tea.Msg) (tea.Cmd, bool) {
	if !c.active {
		return nil, false
	}
	switch msg.(type) {
	case confirm.YesMsg:
		c.active = false
		return c.onYes, true
	case confirm.NoMsg:
		c.active = false
		return nil, true
	case tea.KeyMsg:
		return c.model.Update(msg), true
	}
	return nil, false
}

func (c *confirmer) view() string {
	v := c.viewport.Render(c.model.View())
	w, h := lipgloss.Size(v)
	return util.PlaceOverlay((c.size.Width-w)/2, (c.size.Height-h)/2, v, c.behind)
}

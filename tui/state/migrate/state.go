// Package migrate is the TUI's "mangal migrate": moves data under old provider ids.
package migrate

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/viewport"
	"github.com/luevano/mangal/v5/tui/util"
)

var _ base.State = (*state)(nil)

type state struct {
	// migration is the plan, nil until it's made
	migration *core.Plan
	// plan shows the migration, it can be long
	plan   *viewport.Model
	keyMap keyMap
}

type plannedMsg struct {
	plan core.Plan
}

// migratedMsg is a migration done, relink how many watches need a re-link then.
type migratedMsg struct{ relink int }

// New lists what would be migrated, enter does it.
func New() base.State {
	plan := viewport.New()
	// esc goes back
	plan.KeyMap.Back.SetEnabled(false)
	return &state{plan: plan, keyMap: keyMap{apply: util.Bind("migrate", "enter")}}
}

func (s *state) Key() string     { return "migrate" }
func (s *state) Backable() bool  { return true }
func (s *state) Inputting() bool { return false }
func (s *state) KeyMap() help.KeyMap {
	return base.CombinedKeyMap(util.KeyMap{Short: []key.Binding{s.keyMap.apply}}, s.plan.KeyMap)
}
func (s *state) Title() base.Title             { return base.Title{Text: "Migrate"} }
func (s *state) Status() string                { return "" }
func (s *state) Resize(size base.Size) tea.Cmd { return s.plan.Resize(size) }
func (s *state) Subtitle() string {
	return "data under old provider ids moves to the current ones, the old cache is dropped"
}

func (s *state) Init(ctx context.Context) tea.Cmd {
	return base.PlanChange(ctx, "Looking for old provider ids", core.MigrateReq{}, func(p core.Plan) tea.Msg { return plannedMsg{plan: p} })
}

// empty is true for a plan with nothing to migrate.
func empty(p *core.Plan) bool {
	return p == nil || p.Nothing || len(p.Lines) == 0
}

func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case plannedMsg:
		s.migration = &msg.plan
		s.keyMap.apply.SetEnabled(!empty(s.migration))
		if empty(s.migration) {
			s.plan.SetContent(style.Normal.Secondary.Render("Nothing to migrate."))
		} else {
			s.plan.SetContent(strings.Join(msg.plan.Lines, "\n"))
		}
	case migratedMsg:
		if msg.relink > 0 {
			return tea.Sequence(base.Back, base.NotifyWarning(fmt.Sprintf(
				"Migrated. %d watches need a re-link (the site may use other ids): %s, then %s lists them, %s re-links one", msg.relink,
				base.KeyWatches.Help().Key, base.KeyNeedsRelink.Help().Key, base.KeyRelink.Help().Key)))
		}
		return tea.Sequence(base.Back, base.Notify("Migrated, folders keep their names (mangal library rename renames them)"))
	case tea.KeyPressMsg:
		if !key.Matches(msg, s.keyMap.apply) {
			return s.plan.Update(msg)
		}
		if empty(s.migration) || base.Busy(ctx) {
			return nil
		}
		plan := *s.migration
		return base.LoadWithStatus(ctx, "Migrating", func(ctx context.Context, status func(string)) (migratedMsg, error) {
			if _, err := base.ApplyPlan(ctx, plan, status); err != nil {
				return migratedMsg{}, err
			}
			c, err := base.Core()
			if err != nil {
				return migratedMsg{}, err
			}
			m, err := c.Watches(ctx)
			if err != nil {
				return migratedMsg{}, err
			}
			watches, err := m.List(ctx)
			msg := migratedMsg{}
			for _, w := range watches {
				if w.NeedsRelink {
					msg.relink++
				}
			}
			return msg, err
		}, func(msg migratedMsg) tea.Msg { return msg })
	}
	return nil
}

func (s *state) View() string {
	if s.migration == nil {
		return ""
	}
	return s.plan.View()
}

type keyMap struct {
	apply key.Binding
}

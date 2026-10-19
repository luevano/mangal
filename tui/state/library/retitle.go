package library

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/confirm"
	"github.com/luevano/mangal/v5/tui/state/pick"
)

// planRetitle plans retitling the library manga from the provider's manga. Swapped in
// tests.
var planRetitle = func(ctx context.Context, m storage.Manga, provider string, from mangadata.Manga) (core.Plan, error) {
	c, err := base.Core()
	if err != nil {
		return core.Plan{}, err
	}
	info := from.Info()
	req, err := c.RetitleSource(ctx, m.ID, core.RetitleFrom{Provider: provider, MangaID: info.ID, Query: info.Title})
	if err != nil {
		return core.Plan{}, err
	}
	return c.Changes.Plan(ctx, req)
}

// retitleCmd opens the provider picker of a retitle (the current source's first), the
// provider picked opens the search.
func retitleCmd(m storage.Manga) tea.Cmd {
	current := ""
	if len(m.Sources) != 0 {
		current = m.Sources[0].Provider
	}
	return func() tea.Msg {
		return pick.NewProviders(current, func(provider string) tea.Cmd {
			return tea.Sequence(base.Back, func() tea.Msg { return newRetitleState(m, provider) })
		})
	}
}

var _ base.State = (*retitleState)(nil)

// retitleState searches a provider for the manga to take the chapter titles from: enter
// shows what the retitle does, yes does it and goes back to the library manga.
type retitleState struct {
	*pick.Manga
	confirm.Host
	manga storage.Manga
}

type retitlePlannedMsg struct{ plan core.Plan }

func newRetitleState(m storage.Manga, provider string) *retitleState {
	s := &retitleState{manga: m, Host: confirm.NewHost(72, color.Warning)}
	s.Manga = pick.NewManga(pick.MangaOptions{
		Title:    "Retitle the chapters of " + m.Title,
		Provider: provider,
		Query:    m.Title,
		Action:   "take its titles",
		Current: func(provider, current string, manga mangadata.Manga) bool {
			return slices.ContainsFunc(m.Sources, func(src storage.Source) bool {
				return (src.Provider == provider || src.Provider == current) && src.MangaID == manga.Info().ID
			})
		},
		Pick: s.planCmd,
	})
	return s
}

func (s *retitleState) Key() string { return fmt.Sprintf("library-retitle/%d", s.manga.ID) }

func (s *retitleState) Backable() bool                { return s.Host.Backable(s.Manga.Backable()) }
func (s *retitleState) Inputting() bool               { return s.Host.Inputting(s.Manga.Inputting()) }
func (s *retitleState) KeyMap() help.KeyMap           { return s.Host.KeyMap(s.Manga.KeyMap()) }
func (s *retitleState) Resize(size base.Size) tea.Cmd { return s.Host.Resize(size, s.Manga.Resize) }
func (s *retitleState) View() string                  { return s.Host.View(s.Manga.View) }

func (s *retitleState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	if cmd, ok := s.Answer(msg); ok {
		return cmd
	}
	switch msg := msg.(type) {
	case retitlePlannedMsg:
		return s.ask(msg.plan)
	case actionMsg:
		return msg.run(ctx)
	case reloadMsg:
		return tea.Sequence(base.Back, base.Notify(msg.notify))
	}
	return s.Manga.Update(ctx, msg)
}

func (s *retitleState) planCmd(ctx context.Context, provider string, from mangadata.Manga) tea.Cmd {
	m := s.manga
	return base.LoadWithStatus(ctx, fmt.Sprintf("Listing the chapters of %q", from.Info().Title), func(ctx context.Context, status func(string)) (retitlePlannedMsg, error) {
		plan, err := planRetitle(base.WithStatus(ctx, status), m, provider, from)
		return retitlePlannedMsg{plan: plan}, err
	}, func(msg retitlePlannedMsg) tea.Msg { return msg })
}

// ask confirms the retitle, or says there's nothing to retitle.
func (s *retitleState) ask(plan core.Plan) tea.Cmd {
	if plan.Nothing {
		return base.Notify("Nothing to retitle: " + strings.Join(plan.Lines, "; "))
	}
	s.Confirm.Ask("Retitle", strings.Join(plan.Lines, "\n"), s.View(), func() tea.Msg {
		return actionMsg{label: "Retitling " + s.manga.Title, do: func(ctx context.Context, status func(string)) (reloadMsg, error) {
			res, err := base.ApplyPlan(ctx, plan, status)
			if err != nil {
				return reloadMsg{}, err
			}
			return reloadMsg{notify: undoNotice(fmt.Sprintf("retitled %d chapters of %s", res.Detail, s.manga.Title))}, nil
		}}
	})
	return nil
}

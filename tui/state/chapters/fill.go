package chapters

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
	tuijobs "github.com/luevano/mangal/v5/tui/jobs"
	"github.com/luevano/mangal/v5/tui/model/confirm"
	"github.com/luevano/mangal/v5/tui/state/pick"
)

// planFill plans getting the manga's unavailable chapters from the provider's manga.
// Swapped in tests.
var planFill = func(ctx context.Context, req core.FillReq) (core.FillPlan, error) {
	c, err := base.Core()
	if err != nil {
		return core.FillPlan{}, err
	}
	return c.PlanFill(ctx, req)
}

// fillCmd opens the provider picker of a fill, the provider picked opens the search; only
// for a manga with unavailable chapters.
func (s *state) fillCmd() tea.Cmd {
	if s.client == nil || !slices.ContainsFunc(s.items(), func(i *item) bool { return i.unavailable() != "" }) {
		return base.Notify("No unavailable chapters to get from another provider")
	}
	provider, manga, origin := s.client.Info().ID, s.manga, s.Key()
	return func() tea.Msg {
		return pick.NewProviders("", func(from string) tea.Cmd {
			return tea.Sequence(base.Back, func() tea.Msg { return newFillState(provider, manga, from, origin) })
		})
	}
}

var _ base.State = (*fillState)(nil)

// fillState searches a provider for the manga to get the unavailable chapters from: enter
// shows the plan, yes queues the download and goes back to the chapters.
type fillState struct {
	*pick.Manga
	confirm.Host
	provider, origin string
	manga            mangadata.Manga
}

type fillPlannedMsg struct{ plan core.FillPlan }

func newFillState(provider string, manga mangadata.Manga, from, origin string) *fillState {
	s := &fillState{provider: provider, manga: manga, origin: origin, Host: confirm.NewHost(72, color.Success)}
	s.Manga = pick.NewManga(pick.MangaOptions{
		Title:    "Get the unavailable chapters of " + manga.Info().Title,
		Provider: from,
		Query:    manga.Info().Title,
		Action:   "get its chapters",
		Pick:     s.planCmd,
	})
	return s
}

func (s *fillState) Key() string { return "chapters-fill/" + s.provider + "/" + s.manga.Info().ID }

func (s *fillState) Backable() bool                { return s.Host.Backable(s.Manga.Backable()) }
func (s *fillState) Inputting() bool               { return s.Host.Inputting(s.Manga.Inputting()) }
func (s *fillState) KeyMap() help.KeyMap           { return s.Host.KeyMap(s.Manga.KeyMap()) }
func (s *fillState) Resize(size base.Size) tea.Cmd { return s.Host.Resize(size, s.Manga.Resize) }
func (s *fillState) View() string                  { return s.Host.View(s.Manga.View) }

func (s *fillState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	if cmd, ok := s.Answer(msg); ok {
		return cmd
	}
	if msg, ok := msg.(fillPlannedMsg); ok {
		return s.ask(msg.plan)
	}
	return s.Manga.Update(ctx, msg)
}

func (s *fillState) planCmd(ctx context.Context, provider string, from mangadata.Manga) tea.Cmd {
	req := s.req(provider, from)
	return base.LoadWithStatus(ctx, fmt.Sprintf("Listing the chapters of %q", from.Info().Title), func(ctx context.Context, status func(string)) (fillPlannedMsg, error) {
		plan, err := planFill(base.WithStatus(ctx, status), req)
		return fillPlannedMsg{plan: plan}, err
	}, func(msg fillPlannedMsg) tea.Msg { return msg })
}

// req is the fill of the manga from the provider's manga.
func (s *fillState) req(provider string, from mangadata.Manga) core.FillReq {
	info := from.Info()
	return core.FillReq{Provider: s.provider, MangaID: s.manga.Info().ID, Query: s.manga.Info().Title,
		From: core.RetitleFrom{Provider: provider, MangaID: info.ID, Query: info.Title}}
}

// ask confirms the fill, or says there's nothing to get.
func (s *fillState) ask(plan core.FillPlan) tea.Cmd {
	if plan.Nothing() {
		return base.Notify("Nothing to get: " + strings.Join(plan.Lines(), "; "))
	}
	s.Confirm.Ask("Get chapters", strings.Join(plan.Lines(), "\n"), s.View(), func() tea.Msg {
		tuijobs.Queue(plan.Job(s.origin))
		return base.Back()
	})
	return nil
}

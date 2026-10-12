package library

import (
	"context"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/confirm"
	"github.com/luevano/mangal/v5/tui/state/pick"
)

// newMover is the library's Mover, read when a move is planned. Swapped in tests.
var newMover = client.LibraryMover

// acquireInfo is the provider's info (its current id). Swapped in tests.
var acquireInfo = func(ctx context.Context, provider string) (libmangal.ProviderInfo, error) {
	c, _, err := client.AcquireByID(ctx, provider)
	if err != nil {
		return libmangal.ProviderInfo{}, err
	}
	return c.Info(), nil
}

// moveCmd opens the provider picker of a move, the provider picked opens the search.
func moveCmd(m storage.Manga) tea.Cmd {
	current := ""
	if len(m.Sources) != 0 {
		current = m.Sources[0].Provider
	}
	return func() tea.Msg {
		return pick.NewProviders(current, func(provider string) tea.Cmd {
			return tea.Sequence(base.Back, func() tea.Msg { return newMoveState(m, provider) })
		})
	}
}

var _ base.State = (*moveState)(nil)

// moveState searches a provider for the manga to move a library manga to: enter shows
// what the move does, yes does it and goes back to the library.
type moveState struct {
	*pick.Manga
	manga   storage.Manga
	confirm *confirm.Dialog
}

type movePlannedMsg struct {
	mover library.Mover
	plan  library.MovePlan
}

func newMoveState(m storage.Manga, provider string) *moveState {
	s := &moveState{manga: m, confirm: confirm.NewDialog(72, color.Warning)}
	s.Manga = pick.NewManga(pick.MangaOptions{
		Title:    "Move " + m.Title,
		Provider: provider,
		Query:    m.Title,
		Action:   "move to it",
		Current: func(provider, current string, manga mangadata.Manga) bool {
			return slices.ContainsFunc(m.Sources, func(src storage.Source) bool {
				return (src.Provider == provider || src.Provider == current) && src.MangaID == manga.Info().ID
			})
		},
		Subtitle: func(string) []string {
			var sources []string
			for _, src := range m.Sources {
				sources = append(sources, src.Provider)
			}
			if len(sources) == 0 {
				return []string{"no source yet"}
			}
			return []string{"now " + strings.Join(sources, ", ")}
		},
		Pick: s.planCmd,
	})
	return s
}

func (s *moveState) Backable() bool  { return s.Manga.Backable() && !s.confirm.Active() }
func (s *moveState) Inputting() bool { return s.Manga.Inputting() || s.confirm.Active() }

func (s *moveState) Resize(size base.Size) tea.Cmd {
	s.confirm.Resize(size)
	return s.Manga.Resize(size)
}

func (s *moveState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	if msg, ok := msg.(tea.KeyMsg); ok && s.confirm.Active() {
		return s.confirm.Update(msg)
	}
	switch msg := msg.(type) {
	case movePlannedMsg:
		s.askMove(msg)
		return nil
	case actionMsg:
		return msg.run(ctx)
	case reloadMsg:
		return tea.Sequence(base.Back, base.Notify(msg.notify))
	}
	return s.Manga.Update(ctx, msg)
}

func (s *moveState) planCmd(ctx context.Context, provider string, to mangadata.Manga) tea.Cmd {
	m := s.manga
	return base.Load(ctx, fmt.Sprintf("Planning the move to %q", to.Info().Title), func(ctx context.Context) (movePlannedMsg, error) {
		return planMove(ctx, m, provider, to)
	}, func(msg movePlannedMsg) tea.Msg { return msg })
}

// planMove plans moving the library manga to the provider's manga.
func planMove(ctx context.Context, m storage.Manga, provider string, to mangadata.Manga) (movePlannedMsg, error) {
	mover, err := newMover()
	if err != nil {
		return movePlannedMsg{}, err
	}
	info, err := acquireInfo(ctx, provider)
	if err != nil {
		return movePlannedMsg{}, err
	}
	// the manga may have changed since the list was loaded
	if fresh, ok, err := mover.Library.MangaByID(m.ID); err != nil {
		return movePlannedMsg{}, err
	} else if ok {
		m = fresh
	}
	plan, err := mover.PlanMove(ctx, m, info, to)
	return movePlannedMsg{mover: mover, plan: plan}, err
}

func (s *moveState) askMove(msg movePlannedMsg) {
	plan, mover := msg.plan, msg.mover
	s.confirm.Ask("Move", strings.Join(plan.Lines(), "\n"), s.View(), action("Moving "+plan.Manga.Title, func() (string, error) {
		return fmt.Sprintf("moved %s to %s (%s)", plan.Manga.Title, plan.Provider.ID, plan.To.Title), mover.ApplyMove(plan)
	}))
}

func (s *moveState) View() string {
	if s.confirm.Active() {
		return s.confirm.View()
	}
	return s.Manga.View()
}

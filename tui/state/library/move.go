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

// movePlannedMsg is the planned move, and the full one (adopting the new title) when the
// titles differ, or why it can't be.
type movePlannedMsg struct {
	mover   library.Mover
	plan    library.MovePlan
	full    *library.MovePlan
	fullErr error
}

// moveChosenMsg is the move picked (keeping the title or adopting the new one), to confirm.
type moveChosenMsg struct {
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

func (s *moveState) Key() string { return fmt.Sprintf("library-move/%d", s.manga.ID) }

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
		s.askTitle(msg)
		return nil
	case moveChosenMsg:
		s.askMove(msg.mover, msg.plan)
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
	return base.LoadWithStatus(ctx, fmt.Sprintf("Planning the move to %q", to.Info().Title), func(ctx context.Context, status func(string)) (movePlannedMsg, error) {
		return planMove(ctx, m, provider, to, func(p library.Progress) { status(p.String()) })
	}, func(msg movePlannedMsg) tea.Msg { return msg })
}

// planMove plans moving the library manga to the provider's manga, telling onProgress
// (may be nil) where it is.
func planMove(ctx context.Context, m storage.Manga, provider string, to mangadata.Manga, onProgress func(library.Progress)) (movePlannedMsg, error) {
	mover, err := newMover()
	if err != nil {
		return movePlannedMsg{}, err
	}
	mover.OnProgress = onProgress
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
	plan, err := mover.PlanMove(ctx, m, info, to, library.MoveOptions{})
	if err != nil {
		return movePlannedMsg{}, err
	}
	msg := movePlannedMsg{mover: mover, plan: plan}
	if t := to.Info().Title; t != "" && t != m.Title {
		full, err := mover.PlanMove(ctx, m, info, to, library.MoveOptions{Full: true})
		if err != nil {
			msg.fullErr = err
		} else {
			msg.full = &full
		}
	}
	return msg, nil
}

// askTitle asks whether to keep the library title or adopt the new manga's, when they
// differ; else it confirms the move.
func (s *moveState) askTitle(msg movePlannedMsg) {
	if msg.full == nil && msg.fullErr == nil {
		s.askMove(msg.mover, msg.plan)
		return
	}
	plan := msg.plan
	lines := []string{
		fmt.Sprintf("%q is %q on %s.", plan.Manga.Title, plan.To.Title, plan.Provider.ID),
		"",
		"k keeps the title:",
	}
	indent := func(lines []string) []string {
		for i := range lines {
			lines[i] = "  " + lines[i]
		}
		return lines
	}
	lines = append(lines, indent(plan.FolderLines())...)
	choices := []confirm.Choice{{Key: "k", Label: "keep the title", Cmd: func() tea.Msg { return moveChosenMsg{mover: msg.mover, plan: plan} }}}
	if msg.full != nil {
		full := *msg.full
		lines = append(lines, "a adopts the new title:", "  title: "+full.TitleChange())
		lines = append(lines, indent(full.FolderLines())...)
		choices = append(choices, confirm.Choice{Key: "a", Label: "adopt the new title", Cmd: func() tea.Msg { return moveChosenMsg{mover: msg.mover, plan: full} }})
	} else {
		lines = append(lines, "can't adopt it: "+msg.fullErr.Error())
	}
	s.confirm.Choose("Library title", strings.Join(lines, "\n"), s.View(), choices...)
}

func (s *moveState) askMove(mover library.Mover, plan library.MovePlan) {
	s.confirm.Ask("Move", strings.Join(plan.Lines(), "\n"), s.View(), action("Moving "+plan.Manga.Title, func() (string, error) {
		summary := fmt.Sprintf("move %d %q to %s %s", plan.Manga.ID, plan.Manga.Title, plan.Provider.ID, plan.To.ID)
		return journaled("move", summary, fmt.Sprintf("moved %s to %s (%s)", plan.Manga.Title, plan.Provider.ID, plan.To.Title), func(j library.Journal) error {
			mover.Journal = j
			return mover.ApplyMove(plan)
		})
	}))
}

func (s *moveState) View() string {
	if s.confirm.Active() {
		return s.confirm.View()
	}
	return s.Manga.View()
}

package watches

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/state/pick"
)

var _ base.State = (*relinkState)(nil)

// relinkState searches a provider (the watch's by default) for the manga to point the
// watch to: enter re-links it (watch.Manager.Relink) and goes back.
type relinkState struct {
	*pick.Manga
	manager watch.Manager
	w       watch.Watch
}

// relinkedMsg is the watch re-linked, for the screen the search was opened from.
type relinkedMsg struct{ w watch.Watch }

func newRelinkState(m watch.Manager, w watch.Watch) *relinkState {
	query := w.Title
	if strings.HasPrefix(w.MangaID, storage.PlaceholderPrefix) && query == "" {
		query = strings.TrimPrefix(w.MangaID, storage.PlaceholderPrefix)
	}
	s := &relinkState{manager: m, w: w}
	s.Manga = pick.NewManga(pick.MangaOptions{
		Title:    "Re-link " + w.Title,
		Provider: w.Provider,
		Query:    query,
		Action:   "re-link to it",
		Current: func(provider, current string, manga mangadata.Manga) bool {
			return (current == w.Provider || provider == w.Provider) && manga.Info().ID == w.MangaID
		},
		Subtitle: func(string) []string {
			parts := []string{"now " + w.Provider + " " + w.MangaID}
			if w.NeedsRelink {
				parts = append(parts, "needs re-link: "+w.RelinkReason)
			}
			return parts
		},
		Pick: s.relinkCmd,
	})
	return s
}

func (s *relinkState) Key() string { return fmt.Sprintf("watch-relink/%d", s.w.ID) }

func (s *relinkState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	if msg, ok := msg.(relinkedMsg); ok {
		w := msg.w
		return tea.Sequence(base.Back, func() tea.Msg { return msg },
			base.Notify(fmt.Sprintf("Re-linked to %q from %s (%s)", w.Title, w.Provider, w.MangaID)))
	}
	return s.Manga.Update(ctx, msg)
}

func (s *relinkState) relinkCmd(ctx context.Context, provider string, manga mangadata.Manga) tea.Cmd {
	m, id := s.manager, s.w.ID
	return base.Load(ctx, fmt.Sprintf("Re-linking to %q", manga.Info().Title), func(ctx context.Context) (relinkedMsg, error) {
		return relink(ctx, m, id, provider, manga)
	}, func(msg relinkedMsg) tea.Msg { return msg })
}

// relink points the watch to the provider's manga.
func relink(ctx context.Context, m watch.Manager, id int64, provider string, manga mangadata.Manga) (relinkedMsg, error) {
	info := manga.Info()
	// the title finds it on providers without a lookup by id (and on a server)
	w, err := m.Relink(ctx, id, watch.RelinkOptions{Provider: provider, MangaID: info.ID, Query: info.Title})
	if errors.Is(err, watch.ErrExists) {
		return relinkedMsg{}, fmt.Errorf("another watch has %q: %w", info.Title, err)
	}
	return relinkedMsg{w: w}, err
}

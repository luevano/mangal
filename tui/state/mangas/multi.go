package mangas

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/state/pick"
)

var _ base.State = (*multiState)(nil)

// multiState searches several providers at once, enter opens the manga as the mangas
// screen does. The clients of the providers opened are held until the screen is left.
type multiState struct {
	*pick.Multi
	releases map[string]func()
	// the items point to them
	extraInfo, fullExtraInfo bool
}

// heldMsg is the client of a picked manga's provider, held until release.
type heldMsg struct {
	client  *libmangal.Client
	release func()
	manga   mangadata.Manga
}

// NewMulti is the search of the providers at once.
func NewMulti(providers []libmangal.ProviderInfo) base.State {
	s := &multiState{releases: map[string]func(){}}
	s.Multi = pick.NewMulti(pick.MultiOptions{Title: "Search", Providers: providers, Action: "confirm", Pick: s.holdCmd})
	return s
}

// Destroy ends the search and releases the clients.
func (s *multiState) Destroy() {
	s.Multi.Destroy()
	for _, release := range s.releases {
		release()
	}
}

func (s *multiState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case heldMsg:
		id := msg.client.Info().ID
		if s.releases[id] != nil {
			msg.release()
		} else {
			s.releases[id] = msg.release
		}
		return openCmd(ctx, msg.client, newItem(id, msg.manga, &s.extraInfo, &s.fullExtraInfo), "")
	case openedMsg:
		return onOpened(msg)
	}
	return s.Multi.Update(ctx, msg)
}

// holdCmd holds the provider's client, then opens the manga with it.
func (s *multiState) holdCmd(ctx context.Context, provider string, manga mangadata.Manga) tea.Cmd {
	return base.Load(ctx, fmt.Sprintf("Loading provider %q", provider), func(ctx context.Context) (heldMsg, error) {
		c, _, release, err := client.HoldByID(ctx, provider)
		return heldMsg{client: c, release: release, manga: manga}, err
	}, func(msg heldMsg) tea.Msg {
		// left meanwhile: nobody gets it
		if ctx.Err() != nil {
			msg.release()
			return nil
		}
		return msg
	})
}

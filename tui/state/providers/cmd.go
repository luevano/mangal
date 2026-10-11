package providers

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/state/mangas"
)

func (s *state) loadProviderCmd(ctx context.Context, item *item) tea.Cmd {
	return tea.Sequence(
		base.Loading(fmt.Sprintf("Loading provider %q", item.loader)),
		func() tea.Msg {
			var mangalClient *libmangal.Client
			var newex string
			if c := client.Get(item.loader); c != nil {
				newex = "existing"
				mangalClient = c
			} else {
				newex = "new"
				c, err := client.Acquire(ctx, item.loader)
				if err != nil {
					return err
				}
				// in the background, the anilist screens show the login once it's done
				go func() { _ = anilist.EnsureLogin(ctx) }()
				mangalClient = c
				item.markLoaded()

				mangalClient.Logger().SetOnLog(func(format string, a ...any) {
					// TODO: add option for "verbose" so it logs pages progress?
					if !strings.HasPrefix(format, "page") {
						log.Log(format, a...)
					}
				})
			}
			log.Log("Using %s mangal client for provider %q", newex, item.loader.String())
			return mangas.New(mangalClient)
		},
		base.Loaded,
	)
}

func (s *state) closeAllProvidersCmd() tea.Msg {
	if err := s.closeAll(); err != nil {
		return err
	}
	return base.Notify("Closed all clients")()
}

func (s *state) closeAll() error {
	if err := client.CloseAll(); err != nil {
		return err
	}
	for _, item := range s.loaded.Keys() {
		item.markClosed()
	}
	return nil
}

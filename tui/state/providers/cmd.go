package providers

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/client"
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
				c, err := client.NewClient(ctx, item.loader)
				if err != nil {
					return err
				}
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
	if err := client.CloseAll(); err != nil {
		return func() tea.Msg {
			return err
		}
	}

	for _, item := range s.loaded.Keys() {
		item.markClosed()
	}

	return base.Notify("Closed all clients")()
}

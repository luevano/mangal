package providers

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/state/mangas"
	"github.com/luevano/mangal/v5/tui/util"
)

var _ base.State = (*state)(nil)

type state struct {
	list.Screen
	// the items point to it
	extraInfo bool

	confirm,
	info,
	closeAll,
	mirrors key.Binding
}

// loadedMsg is a provider's client, created now if isNew.
type loadedMsg struct {
	item   *item
	client *libmangal.Client
	isNew  bool
}

type closedMsg struct{}

// reloadedMsg are the providers loaded again, nil if they weren't.
type reloadedMsg struct {
	loaders []libmangal.ProviderLoader
}

func New(loaders []libmangal.ProviderLoader) base.State {
	s := &state{
		confirm:  util.Bind("confirm", "enter"),
		info:     base.KeyInfo,
		closeAll: base.KeyCloseAll,
		mirrors:  base.KeyMirrors,
	}
	s.Screen = list.NewScreen(list.New(2, 1, "provider", "providers", loaders, s.newItem), s.keyMap)
	return s
}

func (s *state) newItem(loader libmangal.ProviderLoader) _list.DefaultItem {
	return &item{loader: loader, extraInfo: &s.extraInfo}
}

func (s *state) keyMap() help.KeyMap {
	return util.KeyMap{
		Short: []key.Binding{s.confirm, s.info},
		Full:  [][]key.Binding{{s.confirm, s.info, s.mirrors, s.closeAll}},
	}
}

func (s *state) Title() base.Title {
	return base.Title{Text: "Providers"}
}

// Init closes the clients left from the last time (back home and in again).
func (s *state) Init(ctx context.Context) tea.Cmd {
	return s.refresh(ctx, base.TakeReloadProviders())
}

// refresh closes the clients, and loads the providers again if reload (the config
// changed their options, see base.ReloadProviders).
func (s *state) refresh(ctx context.Context, reload bool) tea.Cmd {
	label := ""
	if reload {
		label = "Reloading providers"
	}
	return base.Load(ctx, label, func(context.Context) (reloadedMsg, error) {
		if err := client.CloseAll(); err != nil {
			return reloadedMsg{}, err
		}
		if !reload {
			return reloadedMsg{}, nil
		}
		loaders, err := manager.Loaders()
		return reloadedMsg{loaders: loaders}, err
	}, func(msg reloadedMsg) tea.Msg { return msg })
}

func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case loadedMsg:
		if msg.isNew {
			msg.item.loadTime = time.Now()
		}
		return func() tea.Msg { return mangas.New(msg.client) }
	case closedMsg:
		return base.Notify("Closed all clients")
	case base.RestoredMsg:
		if base.TakeReloadProviders() {
			return s.refresh(ctx, true)
		}
	case reloadedMsg:
		if msg.loaders == nil {
			return nil
		}
		items := make([]_list.Item, len(msg.loaders))
		for n, loader := range msg.loaders {
			items[n] = s.newItem(loader)
		}
		selected := s.List.Index()
		cmd := s.List.SetItems(items)
		s.List.Select(selected)
		return tea.Sequence(cmd, base.Notify("Providers reloaded"))
	case tea.KeyMsg:
		if s.List.Filtering() {
			break
		}
		i, ok := s.List.SelectedItem().(*item)
		if !ok {
			break
		}
		switch {
		case key.Matches(msg, s.confirm):
			if base.Busy(ctx) {
				return nil
			}
			return s.loadCmd(ctx, i)
		case key.Matches(msg, s.info):
			s.extraInfo = !s.extraInfo
			if s.extraInfo {
				s.List.SetItemHeight(3)
			} else {
				s.List.SetItemHeight(2)
			}
			return nil
		case key.Matches(msg, s.closeAll):
			return base.Load(ctx, "", func(context.Context) (struct{}, error) {
				return struct{}{}, client.CloseAll()
			}, func(struct{}) tea.Msg { return closedMsg{} })
		case key.Matches(msg, s.mirrors):
			info := i.loader.Info()
			if len(info.Mirrors) == 0 {
				return base.Notify(info.Name + " has no mirrors")
			}
			return func() tea.Msg { return newMirrors(info) }
		}
	}
	return s.List.Update(msg)
}

func (s *state) loadCmd(ctx context.Context, i *item) tea.Cmd {
	return base.Load(ctx, fmt.Sprintf("Loading provider %q", i.loader), func(ctx context.Context) (loadedMsg, error) {
		isNew := client.Get(i.loader) == nil
		c, err := client.Acquire(ctx, i.loader)
		if err != nil {
			return loadedMsg{}, err
		}
		log.Debug("using the client of provider %q (new: %t)", i.loader, isNew)
		return loadedMsg{item: i, client: c, isNew: isNew}, nil
	}, func(msg loadedMsg) tea.Msg { return msg })
}

package providers

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/luevano/mangal/v5/util/cache"
)

var _ base.State = (*mirrorsState)(nil)

// mirrorsState picks the provider's preferred website (providers.mirrors).
type mirrorsState struct {
	list.Screen
	info   libmangal.ProviderInfo
	choose key.Binding
}

type mirrorItem struct {
	url, kind string
	preferred bool
}

func (i mirrorItem) FilterValue() string { return i.url }
func (i mirrorItem) Title() string       { return i.url }
func (i mirrorItem) Description() string {
	if i.preferred {
		return i.kind + ", preferred"
	}
	return i.kind
}

func newMirrors(info libmangal.ProviderInfo) *mirrorsState {
	preferred := config.Providers.Mirrors.Get()[info.ID]
	if preferred == "" {
		preferred = info.Website
	}
	sites := append([]string{info.Website}, info.Mirrors...)
	s := &mirrorsState{
		info:   info,
		choose: util.Bind("prefer", "enter"),
	}
	s.Screen = list.NewScreen(list.New(2, 1, "website", "websites", sites, func(site string) _list.DefaultItem {
		kind := "mirror"
		if site == info.Website {
			kind = "website"
		}
		return mirrorItem{url: site, kind: kind, preferred: site == preferred}
	}), s.keyMap)
	return s
}

func (s *mirrorsState) Key() string { return "mirrors/" + s.info.ID }

func (s *mirrorsState) keyMap() help.KeyMap { return util.KeyMap{Short: []key.Binding{s.choose}} }
func (s *mirrorsState) Title() base.Title   { return base.Title{Text: s.info.Name + " mirrors"} }
func (s *mirrorsState) Subtitle() string    { return "tried in order when the preferred one is down" }

func (s *mirrorsState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	if msg, ok := msg.(tea.KeyMsg); ok && !s.List.Filtering() && key.Matches(msg, s.choose) {
		i, ok := s.List.SelectedItem().(mirrorItem)
		if !ok {
			return nil
		}
		mirror := i.url
		if mirror == s.info.Website {
			// the default, no need to keep it in the config
			mirror = ""
		}
		// in Update, the providers list reads the config to render
		if err := config.SetMirror(s.info.ID, mirror); err != nil {
			return func() tea.Msg { return fmt.Errorf("set the mirror of %s: %w", s.info.Name, err) }
		}
		return tea.Sequence(
			func() tea.Msg {
				// the choice wins over the mirror that last worked
				if err := cache.DeleteMirror(s.info.ID); err != nil {
					return err
				}
				// the loaded providers keep the options they were loaded with
				base.ReloadProviders()
				return base.Back()
			},
			base.Notify(fmt.Sprintf("%s prefers %s", s.info.Name, i.url)),
		)
	}
	return s.List.Update(msg)
}

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
	info   libmangal.ProviderInfo
	list   *list.Model
	choose key.Binding
	// closeAll closes the loaded clients, they keep the options they were loaded with
	closeAll func() error
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

func newMirrors(info libmangal.ProviderInfo, closeAll func() error) *mirrorsState {
	preferred := config.Providers.Mirrors.Get()[info.ID]
	if preferred == "" {
		preferred = info.Website
	}
	sites := append([]string{info.Website}, info.Mirrors...)
	return &mirrorsState{
		info: info,
		list: list.New(2, 1, "website", "websites", sites, func(site string) _list.DefaultItem {
			kind := "mirror"
			if site == info.Website {
				kind = "website"
			}
			return mirrorItem{url: site, kind: kind, preferred: site == preferred}
		}),
		choose:   util.Bind("prefer", "enter"),
		closeAll: closeAll,
	}
}

func (s *mirrorsState) Backable() bool { return s.list.Unfiltered() }
func (s *mirrorsState) KeyMap() help.KeyMap {
	return base.CombinedKeyMap(mirrorsKeyMap{s.choose}, s.list.KeyMap)
}
func (s *mirrorsState) Title() base.Title             { return base.Title{Text: s.info.Name + " mirrors"} }
func (s *mirrorsState) Subtitle() string              { return "tried in order when the preferred one is down" }
func (s *mirrorsState) Status() string                { return s.list.Status() }
func (s *mirrorsState) Resize(size base.Size) tea.Cmd { return s.list.Resize(size) }
func (s *mirrorsState) Init(context.Context) tea.Cmd  { return s.list.Init() }
func (s *mirrorsState) View() string                  { return s.list.View() }

func (s *mirrorsState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	if msg, ok := msg.(tea.KeyMsg); ok && !s.list.Filtering() && key.Matches(msg, s.choose) {
		i, ok := s.list.SelectedItem().(mirrorItem)
		if !ok {
			return nil
		}
		mirror := i.url
		if mirror == s.info.Website {
			// the default, no need to keep it in the config
			mirror = ""
		}
		return tea.Sequence(
			func() tea.Msg {
				if err := config.SetMirror(s.info.ID, mirror); err != nil {
					return err
				}
				// the choice wins over the mirror that last worked
				if err := cache.DeleteMirror(s.info.ID); err != nil {
					return err
				}
				if err := s.closeAll(); err != nil {
					return err
				}
				return base.Back()
			},
			base.Notify(fmt.Sprintf("%s prefers %s", s.info.Name, i.url)),
		)
	}
	return s.list.Update(msg)
}

type mirrorsKeyMap struct{ choose key.Binding }

func (k mirrorsKeyMap) ShortHelp() []key.Binding  { return []key.Binding{k.choose} }
func (k mirrorsKeyMap) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

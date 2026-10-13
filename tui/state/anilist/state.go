// Package anilist is the metadata bindings screen of a manga (AniList, Kitsu, MyAnimeList)
// and the search of a source's manga to bind.
package anilist

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/model/metadata"
	"github.com/luevano/mangal/v5/tui/model/search"
	"github.com/luevano/mangal/v5/tui/model/searchlist"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/luevano/mangal/v5/util/cache"
)

var _ base.State = (*state)(nil)

// state is the search of a source's manga to bind.
type state struct {
	searchlist.Screen
	client   *libmangal.Client
	manga    mangadata.Manga
	src      client.MetadataSource
	provider *lmmeta.ProviderWithCache
	// pick, if set, gets the chosen manga instead of binding it (a library manga's link)
	pick func(lmmeta.Metadata) tea.Cmd

	confirm,
	search,
	metadata key.Binding
}

type resultsMsg []_list.Item

type boundMsg struct {
	src  client.MetadataSource
	meta lmmeta.Metadata
}

// sourceColor is the title color of the source's screens.
func sourceColor(code lmmeta.IDCode) lipgloss.Color {
	switch code {
	case lmmeta.IDCodeKitsu:
		return color.Kitsu
	case lmmeta.IDCodeMyAnimeList:
		return color.MyAnimeList
	}
	return color.Anilist
}

// newSearch is the search of the source's manga to bind the manga to, starting with its
// title. A number (or "id:<n>") gets the manga with that id.
func newSearch(c *libmangal.Client, src client.MetadataSource, provider *lmmeta.ProviderWithCache, manga mangadata.Manga) base.State {
	s := newState(src, provider, manga.Info().Title)
	s.client, s.manga = c, manga
	return s
}

// NewPick is the search of the source's manga for query (a number or "id:<n>" gets that
// id); enter goes back and runs pick with the chosen one.
func NewPick(src client.MetadataSource, provider *lmmeta.ProviderWithCache, query string, pick func(lmmeta.Metadata) tea.Cmd) base.State {
	s := newState(src, provider, query)
	s.pick = pick
	return s
}

func newState(src client.MetadataSource, provider *lmmeta.ProviderWithCache, query string) *state {
	l := list.New(2, 1, src.Key+" manga", src.Key+" mangas", []lmmeta.Metadata{}, func(meta lmmeta.Metadata) _list.DefaultItem {
		return &item{src: src, meta: meta}
	})
	s := &state{
		Screen: searchlist.New(search.New("Search "+src.Name+" manga or id...", query, 64, 5), l, searchlist.History{
			Get: cache.GetAnilistSearchHistory,
			Set: cache.SetAnilistSearchHistory,
		}),
		src:      src,
		provider: provider,
		confirm:  util.Bind("bind", "enter"),
		search:   base.KeySearch,
		metadata: base.KeyMetadata,
	}
	s.updateKeys()
	return s
}

func (s *state) KeyMap() help.KeyMap {
	return base.CombinedKeyMap(util.KeyMap{Short: []key.Binding{s.confirm, s.search, s.metadata}}, s.ListKeyMap())
}

func (s *state) Title() base.Title {
	return base.Title{Text: s.src.Name + " Search", Background: sourceColor(s.src.Code), Foreground: color.Bright}
}

func (s *state) Init(ctx context.Context) tea.Cmd {
	if s.Search.Query() == "" {
		return tea.Batch(s.Screen.Init(ctx), s.Search.Focus())
	}
	return tea.Batch(s.Screen.Init(ctx), s.searchCmd(ctx, s.Search.Query()))
}

func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case resultsMsg:
		cmd := s.SetResults(msg)
		s.updateKeys()
		return cmd
	case boundMsg:
		return tea.Sequence(base.Back, base.Notify(fmt.Sprintf("Bound %s %q", msg.src.Name, msg.meta)))
	case search.SearchMsg:
		return tea.Batch(s.Screen.Update(msg), s.searchCmd(ctx, string(msg)))
	case tea.KeyMsg:
		if s.Inputting() {
			break
		}
		i, ok := s.List.SelectedItem().(*item)
		switch {
		case key.Matches(msg, s.confirm) && ok:
			if base.Busy(ctx) {
				return nil
			}
			if s.pick != nil {
				return tea.Sequence(base.Back, s.pick(i.meta))
			}
			return s.bindCmd(ctx, i.meta)
		case key.Matches(msg, s.search):
			s.List.ResetFilter()
			return s.Search.Focus()
		case key.Matches(msg, s.metadata) && ok:
			return metadata.ShowCmd(i.meta)
		}
	}
	return s.Screen.Update(msg)
}

func (s *state) bindCmd(ctx context.Context, meta lmmeta.Metadata) tea.Cmd {
	c, manga, src := s.client, s.manga, s.src
	return base.Load(ctx, fmt.Sprintf("Binding %s %q", src.Name, meta), func(ctx context.Context) (boundMsg, error) {
		bound, err := client.Bind(ctx, c, c.Info().ID, manga.Info().ID, manga, src.Code, meta.ID().Raw)
		return boundMsg{src: src, meta: bound}, err
	}, func(msg boundMsg) tea.Msg { return msg })
}

var idQuery = regexp.MustCompile(`^\s*(?:id:\s*)?(\d+)\s*$`)

func (s *state) searchCmd(ctx context.Context, query string) tea.Cmd {
	return base.LoadWithStatus(ctx, fmt.Sprintf("Searching %q on %s", query, s.src.Name), func(ctx context.Context, _ func(string)) ([]_list.Item, error) {
		if m := idQuery.FindStringSubmatch(query); m != nil {
			id, _ := strconv.Atoi(m[1])
			meta, found, err := s.provider.SearchByIDContext(ctx, id)
			if err != nil {
				return nil, fmt.Errorf("get %s manga %d: %w", s.src.Name, id, err)
			}
			if !found {
				return nil, nil
			}
			return []_list.Item{&item{src: s.src, meta: meta, score: 1}}, nil
		}

		q := lmmeta.Query{Titles: []string{query}}
		var items []_list.Item

		// the closest on top
		closest, found, err := s.provider.FindClosestContext(ctx, q)
		if err != nil {
			return nil, fmt.Errorf("search %q on %s: %w", query, s.src.Name, err)
		}
		if found {
			items = append(items, &item{src: s.src, meta: closest.Metadata, bad: closest.Bad(), score: closest.Score})
		}

		results, err := s.provider.SearchContext(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("search %q on %s: %w", query, s.src.Name, err)
		}
		for _, manga := range results {
			if found && manga.ID() == closest.Metadata.ID() {
				continue
			}
			score := lmmeta.Score(q, manga)
			items = append(items, &item{src: s.src, meta: manga, bad: score < lmmeta.GoodMatchScore, score: score})
		}
		return items, nil
	}, func(items []_list.Item) tea.Msg { return resultsMsg(items) })
}

func (s *state) updateKeys() {
	enable := len(s.List.Items()) != 0
	s.confirm.SetEnabled(enable)
	s.metadata.SetEnabled(enable)
}

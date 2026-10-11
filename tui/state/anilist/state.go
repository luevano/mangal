// Package anilist is the Anilist search of a manga, to bind its metadata.
package anilist

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/log"
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

type state struct {
	searchlist.Screen
	manga   mangadata.Manga
	anilist *lmmeta.ProviderWithCache

	confirm,
	search,
	metadata key.Binding
}

type resultsMsg []_list.Item

// Open shows the Anilist search for the manga, to bind its metadata.
func Open(client *libmangal.Client, manga mangadata.Manga) tea.Cmd {
	ani, err := client.GetMetadataProvider(lmmeta.IDSourceAnilist)
	if err != nil {
		return func() tea.Msg { return err }
	}
	return func() tea.Msg { return New(ani, manga) }
}

// New is the Anilist search for the manga, starting with its title.
func New(anilist *lmmeta.ProviderWithCache, manga mangadata.Manga) base.State {
	l := list.New(2, 1, "anilist manga", "anilist mangas", []lmmeta.Metadata{}, func(meta lmmeta.Metadata) _list.DefaultItem {
		return &item{meta: meta}
	})
	s := &state{
		Screen: searchlist.New(search.New("Search anilist manga...", manga.Info().Title, 64, 5), l, searchlist.History{
			Get: cache.GetAnilistSearchHistory,
			Set: cache.SetAnilistSearchHistory,
		}),
		manga:    manga,
		anilist:  anilist,
		confirm:  util.Bind("confirm", "enter"),
		search:   util.Bind("search", "s"),
		metadata: util.Bind("metadata", "m"),
	}
	s.updateKeys()
	return s
}

func (s *state) KeyMap() help.KeyMap {
	return base.CombinedKeyMap(util.KeyMap{Short: []key.Binding{s.confirm, s.search, s.metadata}}, s.ListKeyMap())
}

func (s *state) Title() base.Title {
	return base.Title{Text: "Anilist Search", Background: color.Anilist, Foreground: color.Bright}
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
	case search.SearchMsg:
		return tea.Batch(s.Screen.Update(msg), s.searchCmd(ctx, string(msg)))
	case tea.KeyMsg:
		if s.Inputting() {
			break
		}
		i, ok := s.List.SelectedItem().(*item)
		switch {
		case key.Matches(msg, s.confirm) && ok:
			s.manga.SetMetadata(i.meta)
			log.Info("set anilist %q to manga %q", i.meta, s.manga)
			return base.Notify(fmt.Sprintf("Set Anilist %q", i.meta))
		case key.Matches(msg, s.search):
			s.List.ResetFilter()
			return s.Search.Focus()
		case key.Matches(msg, s.metadata) && ok:
			return metadata.ShowCmd(i.meta)
		}
	}
	return s.Screen.Update(msg)
}

func (s *state) searchCmd(ctx context.Context, query string) tea.Cmd {
	return base.Load(ctx, fmt.Sprintf("Searching %q on Anilist", query), func(ctx context.Context) ([]_list.Item, error) {
		q := lmmeta.Query{Titles: []string{query}}
		var items []_list.Item

		// the closest on top
		closest, found, err := s.anilist.FindClosestContext(ctx, q)
		if err != nil {
			return nil, fmt.Errorf("search %q on anilist: %w", query, err)
		}
		if found {
			items = append(items, &item{meta: closest.Metadata, bad: closest.Bad(), score: closest.Score})
		}

		results, err := s.anilist.SearchContext(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("search %q on anilist: %w", query, err)
		}
		for _, manga := range results {
			if found && manga.ID() == closest.Metadata.ID() {
				continue
			}
			score := lmmeta.Score(q, manga)
			items = append(items, &item{meta: manga, bad: score < lmmeta.GoodMatchScore, score: score})
		}
		return items, nil
	}, func(items []_list.Item) tea.Msg { return resultsMsg(items) })
}

func (s *state) updateKeys() {
	enable := len(s.List.Items()) != 0
	s.confirm.SetEnabled(enable)
	s.metadata.SetEnabled(enable)
}

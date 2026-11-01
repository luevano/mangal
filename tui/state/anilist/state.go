// Package anilist is the metadata bindings screen of a manga (AniList, Kitsu, MyAnimeList,
// MangaUpdates) and the search of the manga to bind.
package anilist

import (
	gocolor "image/color"

	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	_list "charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/core"
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

// searchSource is a metadata source the search asks.
type searchSource struct {
	src      client.MetadataSource
	provider client.MetadataSearcher
}

// state is the search of the manga to bind: every source at once, each one's results show
// as they come.
type state struct {
	searchlist.Screen
	client  *libmangal.Client
	manga   mangadata.Manga
	sources []searchSource
	// pick, if set, gets the chosen manga instead of binding it (a library manga's link)
	pick func(lmmeta.Metadata) tea.Cmd
	// key is the window's, empty for a pick (it's for whoever opened it)
	key string

	// query is the latest search, its results so far and each source's progress, by code
	query    string
	items    []_list.Item
	progress map[lmmeta.IDCode]string
	// cancel stops the latest search: a slow source's rate limit isn't spent on an old query
	cancel context.CancelFunc
	// binding is true while a pick is being bound (Busy is true for any search left)
	binding atomic.Bool

	confirm,
	search,
	metadata key.Binding
}

// searching is the progress of a source that didn't answer yet.
const searching = "searching…"

// sourceResultsMsg is a source's results of a search, numbered by NewSearch.
type sourceResultsMsg struct {
	search int
	src    client.MetadataSource
	items  []_list.Item
	err    error
}

type boundMsg struct {
	src  client.MetadataSource
	meta lmmeta.Metadata
}

// sourceColor is the title color of the source's screens.
func sourceColor(code lmmeta.IDCode) gocolor.Color {
	switch code {
	case lmmeta.IDCodeKitsu:
		return color.Kitsu
	case lmmeta.IDCodeMyAnimeList:
		return color.MyAnimeList
	case lmmeta.IDCodeMangaUpdates:
		return color.MangaUpdates
	}
	return color.Anilist
}

// newSearch is the search of the manga to bind the manga to on every source of the client,
// starting with its title. A number (or "id:<n>") gets the manga with that id.
func newSearch(c *libmangal.Client, manga mangadata.Manga) base.State {
	var sources []searchSource
	for _, src := range client.MetadataSources {
		// a source the client doesn't have isn't enabled or configured
		if p, err := c.GetMetadataProvider(src.Source); err == nil {
			sources = append(sources, searchSource{src: src, provider: p})
		}
	}
	s := newState(sources, manga.Info().Title)
	s.client, s.manga = c, manga
	s.key = "metadata-search/" + c.Info().ID + "/" + manga.Info().ID
	return s
}

// NewPick is the search of the source's manga for query (a number or "id:<n>" gets that
// id); enter goes back and runs pick with the chosen one.
func NewPick(src client.MetadataSource, provider *lmmeta.ProviderWithCache, query string, pick func(lmmeta.Metadata) tea.Cmd) base.State {
	s := newState([]searchSource{{src: src, provider: provider}}, query)
	s.pick = pick
	return s
}

func newState(sources []searchSource, query string) *state {
	l := list.New(2, 1, "manga", "mangas", []lmmeta.Metadata{}, func(meta lmmeta.Metadata) _list.DefaultItem {
		return &item{src: sources[0].src, meta: meta}
	})
	placeholder := "Search manga or id..."
	if len(sources) == 1 {
		placeholder = "Search " + sources[0].src.Name + " manga or id..."
	}
	s := &state{
		Screen: searchlist.New(search.New(placeholder, query, 64, 5), l, searchlist.History{
			Get: cache.GetAnilistSearchHistory,
			Set: cache.SetAnilistSearchHistory,
		}),
		sources:  sources,
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

func (s *state) Key() string { return s.key }

func (s *state) Title() base.Title {
	if len(s.sources) == 1 {
		src := s.sources[0].src
		return base.Title{Text: src.Name + " Search", Background: sourceColor(src.Code), Foreground: color.Bright}
	}
	return base.Title{Text: "Metadata Search", Background: color.Anilist, Foreground: color.Bright}
}

// Subtitle is how each source is doing: its results, still searching or failed.
func (s *state) Subtitle() string {
	if s.progress == nil {
		return ""
	}
	parts := make([]string, len(s.sources))
	for i, source := range s.sources {
		parts[i] = source.src.Name + " " + s.progress[source.src.Code]
	}
	return strings.Join(parts, " · ")
}

func (s *state) Init(ctx context.Context) tea.Cmd {
	if s.Search.Query() == "" {
		return tea.Batch(s.Screen.Init(ctx), s.Search.Focus())
	}
	return tea.Batch(s.Screen.Init(ctx), s.searchCmd(ctx, s.Search.Query()))
}

func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case sourceResultsMsg:
		if !s.Latest(msg.search) {
			return nil
		}
		var notice tea.Cmd
		if msg.err != nil {
			log.Warn("%s", msg.err)
			s.progress[msg.src.Code] = "failed"
			notice = base.NotifyWarning(msg.err.Error())
		} else {
			s.progress[msg.src.Code] = strconv.Itoa(len(msg.items))
		}
		s.items = append(s.items, msg.items...)
		cmd := s.KeepResults(s.items)
		s.updateKeys()
		return tea.Batch(cmd, notice)
	case base.RestoredMsg:
		// shown again: what was running was dropped with the old ctx
		if slices.Contains(slices.Collect(maps.Values(s.progress)), searching) {
			return s.searchCmd(ctx, s.query)
		}
		return nil
	case boundMsg:
		return tea.Sequence(base.Back, base.Notify(fmt.Sprintf("Bound %s %q", msg.src.Name, msg.meta)))
	case search.SearchMsg:
		return tea.Batch(s.Screen.Update(msg), s.searchCmd(ctx, string(msg)))
	case tea.KeyPressMsg:
		if s.Inputting() {
			break
		}
		i, ok := s.List.SelectedItem().(*item)
		switch {
		case key.Matches(msg, s.confirm) && ok:
			if s.pick != nil {
				return tea.Sequence(base.Back, s.pick(i.meta))
			}
			return s.bindCmd(ctx, i)
		case key.Matches(msg, s.search):
			s.List.ResetFilter()
			return s.Search.Focus()
		case key.Matches(msg, s.metadata) && ok:
			return metadata.ShowCmd(i.meta)
		}
	}
	return s.Screen.Update(msg)
}

func (s *state) bindCmd(ctx context.Context, i *item) tea.Cmd {
	if !s.binding.CompareAndSwap(false, true) {
		return nil
	}
	c, manga, src := s.client, s.manga, i.src
	return base.Load(ctx, fmt.Sprintf("Binding %s %q", src.Name, i.meta), func(ctx context.Context) (boundMsg, error) {
		defer s.binding.Store(false)
		// no manga: its metadata is only set in Update (a download may be reading it), the
		// bindings screen sets the default one when it's shown again
		res, err := base.ApplyChange(ctx, core.BindReq{Provider: c.Info().ID, MangaID: manga.Info().ID, Code: src.Code, ID: i.meta.ID().Raw}, func(string) {})
		bound, _ := res.Detail.(lmmeta.Metadata)
		return boundMsg{src: src, meta: bound}, err
	}, func(msg boundMsg) tea.Msg { return msg })
}

// searchCmd searches every source at once: a slow or failing one doesn't hold up the others.
func (s *state) searchCmd(ctx context.Context, query string) tea.Cmd {
	n := s.NewSearch()
	if s.cancel != nil {
		s.cancel()
	}
	ctx, s.cancel = context.WithCancel(ctx)
	s.query, s.items = query, nil
	s.progress = map[lmmeta.IDCode]string{}
	cmds := []tea.Cmd{s.List.SetItems(nil)}
	s.updateKeys()
	for _, source := range s.sources {
		s.progress[source.src.Code] = searching
		cmds = append(cmds, searchSourceCmd(ctx, n, source, query))
	}
	return tea.Batch(cmds...)
}

// searchSourceCmd is a source's part of a search. Its error stays in its msg: Load's would
// replace the screen with an error, over the other sources' results.
func searchSourceCmd(ctx context.Context, n int, source searchSource, query string) tea.Cmd {
	src := source.src
	return base.LoadWithStatus(ctx, fmt.Sprintf("Searching %q on %s", query, src.Name), func(ctx context.Context, _ func(string)) (sourceResultsMsg, error) {
		msg := sourceResultsMsg{search: n, src: src}
		hits, err := client.SearchMetadataSource(ctx, src, source.provider, query)
		msg.err = err
		for _, h := range hits {
			msg.items = append(msg.items, &item{src: src, meta: h.Meta, bad: h.Bad, score: h.Score})
		}
		return msg, nil
	}, func(msg sourceResultsMsg) tea.Msg { return msg })
}

func (s *state) updateKeys() {
	enable := len(s.List.Items()) != 0
	s.confirm.SetEnabled(enable)
	s.metadata.SetEnabled(enable)
}

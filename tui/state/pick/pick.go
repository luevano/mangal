// Package pick has the screens that pick a provider's manga for something else (a watch's
// re-link, a library manga's move): a provider picker and a search.
package pick

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	_list "charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/selector"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/model/search"
	"github.com/luevano/mangal/v5/tui/model/searchlist"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/luevano/mangal/v5/util/cache"
)

// SearchProvider searches the provider's mangas, or gets the one with the id
// (selector.Search), current is its current id. Swapped in tests.
var SearchProvider = func(ctx context.Context, provider, id, query string) (mangas []mangadata.Manga, current string, err error) {
	c, _, release, err := client.HoldByID(ctx, provider)
	if err != nil {
		return nil, "", err
	}
	defer release()
	mangas, err = selector.Search(ctx, client.Searcher(c), id, query)
	return mangas, c.Info().ID, err
}

// InstalledProviders are the installed providers. Swapped in tests.
var InstalledProviders = func() ([]libmangal.ProviderInfo, error) {
	return core.NewProviders(nil).List(context.Background())
}

// MangaItem is a search result.
type MangaItem struct {
	Manga mangadata.Manga
	// Current is the manga it's for already (the watch's, a source of the library manga).
	Current bool
}

func (i *MangaItem) FilterValue() string { return i.Manga.Info().Title }

func (i *MangaItem) Title() string {
	title := i.Manga.Info().Title
	if i.Current {
		title += " (current)"
	}
	return title
}

func (i *MangaItem) Description() string {
	info := i.Manga.Info()
	desc := "id " + info.ID
	if info.URL != "" {
		desc += " · " + info.URL
	}
	return desc
}

// MangaOptions say what a Manga screen searches and what enter does.
type MangaOptions struct {
	Title    string
	Provider string
	Query    string
	// Action is the help of enter.
	Action string
	// Current tells the manga it's for already, provider is the searched one and current
	// its current id. May be nil.
	Current func(provider, current string, manga mangadata.Manga) bool
	// Subtitle are more parts of the subtitle (provider is the searched one). May be nil.
	Subtitle func(provider string) []string
	// Pick is what enter does with the selected manga of the provider (its current id).
	Pick func(ctx context.Context, provider string, manga mangadata.Manga) tea.Cmd
}

var _ base.State = (*Manga)(nil)

// Manga searches a provider for a manga: enter picks it (MangaOptions.Pick), tab searches
// another provider.
type Manga struct {
	searchlist.Screen
	opts MangaOptions
	// provider is the one searched, its current id once searched
	provider string

	Assign                   key.Binding
	searchKey, pickKey, byID key.Binding
}

// ResultsMsg are the results of a search.
type ResultsMsg struct {
	Provider string
	Items    []_list.Item
	// Search is its number (searchlist.Screen.NewSearch)
	Search int
}

// ProviderPickedMsg is the provider picked for the search.
type ProviderPickedMsg struct{ Provider string }

// NewManga is the search, of opts.Query when it's set.
func NewManga(opts MangaOptions) *Manga {
	l := list.New(2, 1, "manga", "mangas", []*MangaItem{}, func(i *MangaItem) _list.DefaultItem { return i })
	s := &Manga{
		Screen: searchlist.New(search.New("Search the manga...", opts.Query, 64, 5), l, searchlist.History{
			Get: cache.GetMangasSearchHistory,
			Set: cache.SetMangasSearchHistory,
		}),
		opts:      opts,
		provider:  opts.Provider,
		Assign:    util.Bind(opts.Action, "enter"),
		searchKey: base.KeySearch,
		pickKey:   base.KeyPickProvider,
		byID:      base.KeyByID,
	}
	s.updateKeys()
	return s
}

// Provider is the provider searched, its current id once searched.
func (s *Manga) Provider() string { return s.provider }

// Keys are the search's own keys, without the list's.
func (s *Manga) Keys() help.KeyMap {
	return util.KeyMap{Short: []key.Binding{s.Assign, s.searchKey, s.pickKey, s.byID}}
}

func (s *Manga) KeyMap() help.KeyMap {
	return base.CombinedKeyMap(s.Keys(), s.ListKeyMap())
}

func (s *Manga) Title() base.Title { return base.Title{Text: s.opts.Title} }

func (s *Manga) Subtitle() string {
	parts := []string{"on " + s.provider}
	if s.opts.Subtitle != nil {
		parts = append(parts, s.opts.Subtitle(s.provider)...)
	}
	if sub := s.Screen.Subtitle(); sub != "" {
		parts = append([]string{sub}, parts...)
	}
	return strings.Join(parts, " · ")
}

func (s *Manga) Init(ctx context.Context) tea.Cmd {
	if s.Search.Query() == "" {
		return tea.Batch(s.Screen.Init(ctx), s.Search.Focus())
	}
	return tea.Batch(s.Screen.Init(ctx), s.searchCmd(ctx, s.Search.Query()))
}

func (s *Manga) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case ResultsMsg:
		// a late one would re-link or move to a manga of the search before
		if !s.Latest(msg.Search) {
			return nil
		}
		s.provider = msg.Provider
		cmd := s.SetResults(msg.Items)
		s.updateKeys()
		return cmd
	case ProviderPickedMsg:
		s.provider = msg.Provider
		if q := s.Search.Query(); q != "" {
			return s.searchCmd(ctx, q)
		}
		return s.Search.Focus()
	case search.SearchMsg:
		return tea.Batch(s.Screen.Update(msg), s.searchCmd(ctx, string(msg)))
	case tea.KeyPressMsg:
		// while typing too
		if key.Matches(msg, s.byID) && !s.List.Filtering() {
			return s.ToggleByID()
		}
		if s.Inputting() {
			break
		}
		i, ok := s.List.SelectedItem().(*MangaItem)
		switch {
		case key.Matches(msg, s.Assign) && ok:
			if base.Busy(ctx) {
				return nil
			}
			return s.opts.Pick(ctx, s.provider, i.Manga)
		case key.Matches(msg, s.searchKey):
			s.List.ResetFilter()
			return s.Search.Focus()
		case key.Matches(msg, s.pickKey):
			current := s.provider
			return func() tea.Msg { return NewProviders(current, nil) }
		}
	}
	return s.Screen.Update(msg)
}

func (s *Manga) searchCmd(ctx context.Context, query string) tea.Cmd {
	provider, n := s.provider, s.NewSearch()
	id, title, note := s.MangaQuery(query)
	label := fmt.Sprintf("Searching %q on %s", title, provider)
	if id != "" {
		label = fmt.Sprintf("Getting manga %q from %s", id, provider)
	}
	var noteCmd tea.Cmd
	if note != "" {
		noteCmd = base.Notify(note)
	}
	return tea.Batch(noteCmd, base.LoadWithStatus(ctx, label, func(ctx context.Context, _ func(string)) (ResultsMsg, error) {
		return s.Results(ctx, provider, id, title)
	}, func(msg ResultsMsg) tea.Msg {
		msg.Search = n
		return msg
	}))
}

// Results searches the provider (or gets the manga with the id), marking the current manga.
func (s *Manga) Results(ctx context.Context, provider, id, query string) (ResultsMsg, error) {
	mangas, current, err := SearchProvider(ctx, provider, id, query)
	if err != nil {
		return ResultsMsg{}, fmt.Errorf("%s: %w", provider, err)
	}
	items := make([]_list.Item, len(mangas))
	for i, m := range mangas {
		items[i] = &MangaItem{Manga: m, Current: s.opts.Current != nil && s.opts.Current(provider, current, m)}
	}
	return ResultsMsg{Provider: current, Items: items}, nil
}

func (s *Manga) updateKeys() {
	s.Assign.SetEnabled(len(s.List.Items()) != 0)
}

var _ base.State = (*Providers)(nil)

// providerItem is an installed provider of the picker.
type providerItem struct {
	info    libmangal.ProviderInfo
	current bool
}

func (i *providerItem) FilterValue() string { return i.info.ID + " " + i.info.Name }
func (i *providerItem) Title() string {
	if i.current {
		return i.info.Name + " (current)"
	}
	return i.info.Name
}
func (i *providerItem) Description() string { return i.info.ID }

// Providers picks an installed provider.
type Providers struct {
	list.Screen
	current string
	pick    func(provider string) tea.Cmd
	choose  key.Binding
}

type providersMsg []libmangal.ProviderInfo

// NewProviders picks a provider, current is selected first. pick is what enter does with
// it; nil goes back with a ProviderPickedMsg (for the search it was opened from).
func NewProviders(current string, pick func(provider string) tea.Cmd) *Providers {
	if pick == nil {
		pick = func(provider string) tea.Cmd {
			return tea.Sequence(base.Back, func() tea.Msg { return ProviderPickedMsg{Provider: provider} })
		}
	}
	s := &Providers{current: current, pick: pick, choose: util.Bind("search it", "enter")}
	s.Screen = list.NewScreen(list.New(2, 1, "provider", "providers", []*providerItem{}, func(i *providerItem) _list.DefaultItem { return i }), s.Keys)
	return s
}

// Keys are the picker's own keys, without the list's.
func (s *Providers) Keys() help.KeyMap {
	return util.KeyMap{Short: []key.Binding{s.choose}}
}

func (s *Providers) Title() base.Title { return base.Title{Text: "Provider"} }

func (s *Providers) Init(ctx context.Context) tea.Cmd {
	return base.Load(ctx, "Loading the providers", func(context.Context) (providersMsg, error) {
		infos, err := InstalledProviders()
		return providersMsg(infos), err
	}, func(msg providersMsg) tea.Msg { return msg })
}

func (s *Providers) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case providersMsg:
		items := make([]_list.Item, len(msg))
		index := 0
		for i, info := range msg {
			current := info.Matches(s.current)
			if current {
				index = i
			}
			items[i] = &providerItem{info: info, current: current}
		}
		cmd := s.List.SetItems(items)
		s.List.Select(index)
		return cmd
	case tea.KeyPressMsg:
		if s.List.Filtering() {
			break
		}
		if i, ok := s.List.SelectedItem().(*providerItem); ok && key.Matches(msg, s.choose) {
			return s.pick(i.info.ID)
		}
	}
	return s.List.Update(msg)
}

package watches

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/selector"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/model/search"
	"github.com/luevano/mangal/v5/tui/model/searchlist"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/luevano/mangal/v5/util/cache"
)

// searchProvider searches the provider's mangas (a "mid:<id>" query gets that manga),
// provider is its current id. Swapped in tests.
var searchProvider = func(ctx context.Context, provider, query string) (mangas []mangadata.Manga, current string, err error) {
	c, _, err := client.AcquireByID(ctx, provider)
	if err != nil {
		return nil, "", err
	}
	current = c.Info().ID
	if id, ok := selector.IDQuery(query); ok {
		manga, found, err := c.MangaByIDContext(ctx, id)
		switch {
		case errors.Is(err, libmangal.ErrMangaByIDUnsupported):
			// mangoprovider's searches take "mid:" queries too
		case err != nil:
			return nil, current, err
		case !found:
			return nil, current, nil
		default:
			return []mangadata.Manga{manga}, current, nil
		}
	}
	mangas, err = c.SearchMangasContext(ctx, query)
	return mangas, current, err
}

// installedProviders are the ids and names of the installed providers. Swapped in tests.
var installedProviders = func() ([]libmangal.ProviderInfo, error) {
	loaders, err := manager.Loaders()
	if err != nil {
		return nil, err
	}
	infos := make([]libmangal.ProviderInfo, len(loaders))
	for i, l := range loaders {
		infos[i] = l.Info()
	}
	return infos, nil
}

// mangaItem is a search result of the re-link screen.
type mangaItem struct {
	manga mangadata.Manga
	// current is the watch's manga
	current bool
}

func (i *mangaItem) FilterValue() string { return i.manga.Info().Title }

func (i *mangaItem) Title() string {
	title := i.manga.Info().Title
	if i.current {
		title += " (current)"
	}
	return title
}

func (i *mangaItem) Description() string {
	info := i.manga.Info()
	desc := "id " + info.ID
	if info.URL != "" {
		desc += " · " + info.URL
	}
	return desc
}

var _ base.State = (*relinkState)(nil)

// relinkState searches a provider (the watch's by default) for the manga to point the
// watch to: enter re-links it (watch.Manager.Relink) and goes back.
type relinkState struct {
	searchlist.Screen
	manager watch.Manager
	w       watch.Watch
	// provider is the one searched, its current id once searched
	provider string

	assign, search, pick key.Binding
}

type relinkResultsMsg struct {
	provider string
	items    []_list.Item
}

// relinkedMsg is the watch re-linked, for the screen the search was opened from.
type relinkedMsg struct{ w watch.Watch }

// providerPickedMsg is the provider picked for the re-link search.
type providerPickedMsg struct{ provider string }

func newRelinkState(m watch.Manager, w watch.Watch) *relinkState {
	l := list.New(2, 1, "manga", "mangas", []*mangaItem{}, func(i *mangaItem) _list.DefaultItem { return i })
	query := w.Title
	if strings.HasPrefix(w.MangaID, storage.PlaceholderPrefix) && query == "" {
		query = strings.TrimPrefix(w.MangaID, storage.PlaceholderPrefix)
	}
	s := &relinkState{
		Screen: searchlist.New(search.New("Search the manga, or mid:<id>...", query, 64, 5), l, searchlist.History{
			Get: cache.GetMangasSearchHistory,
			Set: cache.SetMangasSearchHistory,
		}),
		manager:  m,
		w:        w,
		provider: w.Provider,
		assign:   util.Bind("re-link to it", "enter"),
		search:   util.Bind("search", "s"),
		pick:     util.Bind("provider", "p"),
	}
	s.updateKeys()
	return s
}

func (s *relinkState) KeyMap() help.KeyMap {
	return base.CombinedKeyMap(util.KeyMap{Short: []key.Binding{s.assign, s.search, s.pick}}, s.ListKeyMap())
}

func (s *relinkState) Title() base.Title { return base.Title{Text: "Re-link " + s.w.Title} }

func (s *relinkState) Subtitle() string {
	parts := []string{"on " + s.provider, "now " + s.w.Provider + " " + s.w.MangaID}
	if s.w.NeedsRelink {
		parts = append(parts, "needs re-link: "+s.w.RelinkReason)
	}
	if sub := s.Screen.Subtitle(); sub != "" {
		parts = append([]string{sub}, parts...)
	}
	return strings.Join(parts, " · ")
}

func (s *relinkState) Init(ctx context.Context) tea.Cmd {
	if s.Search.Query() == "" {
		return tea.Batch(s.Screen.Init(ctx), s.Search.Focus())
	}
	return tea.Batch(s.Screen.Init(ctx), s.searchCmd(ctx, s.Search.Query()))
}

func (s *relinkState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case relinkResultsMsg:
		s.provider = msg.provider
		cmd := s.SetResults(msg.items)
		s.updateKeys()
		return cmd
	case providerPickedMsg:
		s.provider = msg.provider
		if q := s.Search.Query(); q != "" {
			return s.searchCmd(ctx, q)
		}
		return s.Search.Focus()
	case relinkedMsg:
		w := msg.w
		return tea.Sequence(base.Back, func() tea.Msg { return msg },
			base.Notify(fmt.Sprintf("Re-linked to %q from %s (%s)", w.Title, w.Provider, w.MangaID)))
	case search.SearchMsg:
		return tea.Batch(s.Screen.Update(msg), s.searchCmd(ctx, string(msg)))
	case tea.KeyMsg:
		if s.Inputting() {
			break
		}
		i, ok := s.List.SelectedItem().(*mangaItem)
		switch {
		case key.Matches(msg, s.assign) && ok:
			if base.Busy(ctx) {
				return nil
			}
			return s.relinkCmd(ctx, i.manga)
		case key.Matches(msg, s.search):
			s.List.ResetFilter()
			return s.Search.Focus()
		case key.Matches(msg, s.pick):
			current := s.provider
			return func() tea.Msg { return newProviderPick(current) }
		}
	}
	return s.Screen.Update(msg)
}

func (s *relinkState) searchCmd(ctx context.Context, query string) tea.Cmd {
	provider, w := s.provider, s.w
	return base.Load(ctx, fmt.Sprintf("Searching %q on %s", query, provider), func(ctx context.Context) (relinkResultsMsg, error) {
		return relinkSearch(ctx, w, provider, query)
	}, func(msg relinkResultsMsg) tea.Msg { return msg })
}

// relinkSearch searches the provider for the watch's new manga.
func relinkSearch(ctx context.Context, w watch.Watch, provider, query string) (relinkResultsMsg, error) {
	mangas, current, err := searchProvider(ctx, provider, query)
	if err != nil {
		return relinkResultsMsg{}, fmt.Errorf("search %q on %s: %w", query, provider, err)
	}
	items := make([]_list.Item, len(mangas))
	for i, m := range mangas {
		items[i] = &mangaItem{manga: m, current: (current == w.Provider || provider == w.Provider) && m.Info().ID == w.MangaID}
	}
	return relinkResultsMsg{provider: current, items: items}, nil
}

func (s *relinkState) relinkCmd(ctx context.Context, manga mangadata.Manga) tea.Cmd {
	m, id, provider := s.manager, s.w.ID, s.provider
	return base.Load(ctx, fmt.Sprintf("Re-linking to %q", manga.Info().Title), func(ctx context.Context) (relinkedMsg, error) {
		return relink(ctx, m, id, provider, manga)
	}, func(msg relinkedMsg) tea.Msg { return msg })
}

// relink points the watch to the provider's manga.
func relink(ctx context.Context, m watch.Manager, id int64, provider string, manga mangadata.Manga) (relinkedMsg, error) {
	info := manga.Info()
	// the title finds it on providers without a lookup by id (and on a server)
	w, err := m.Relink(ctx, id, watch.RelinkOptions{Provider: provider, MangaID: info.ID, Query: info.Title})
	if errors.Is(err, watch.ErrExists) {
		return relinkedMsg{}, fmt.Errorf("another watch has %q: %w", info.Title, err)
	}
	return relinkedMsg{w: w}, err
}

func (s *relinkState) updateKeys() {
	s.assign.SetEnabled(len(s.List.Items()) != 0)
}

var _ base.State = (*providerPick)(nil)

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

// providerPick picks the provider of the re-link search.
type providerPick struct {
	list.Screen
	current string
	choose  key.Binding
}

type providersMsg []libmangal.ProviderInfo

func newProviderPick(current string) *providerPick {
	s := &providerPick{current: current, choose: util.Bind("search it", "enter")}
	s.Screen = list.NewScreen(list.New(2, 1, "provider", "providers", []*providerItem{}, func(i *providerItem) _list.DefaultItem { return i }), s.keyMap)
	return s
}

func (s *providerPick) keyMap() help.KeyMap {
	return util.KeyMap{Short: []key.Binding{s.choose}}
}

func (s *providerPick) Title() base.Title { return base.Title{Text: "Provider"} }

func (s *providerPick) Init(ctx context.Context) tea.Cmd {
	return base.Load(ctx, "Loading the providers", func(context.Context) (providersMsg, error) {
		infos, err := installedProviders()
		return providersMsg(infos), err
	}, func(msg providersMsg) tea.Msg { return msg })
}

func (s *providerPick) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
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
	case tea.KeyMsg:
		if s.List.Filtering() {
			break
		}
		if i, ok := s.List.SelectedItem().(*providerItem); ok && key.Matches(msg, s.choose) {
			id := i.info.ID
			return tea.Sequence(base.Back, func() tea.Msg { return providerPickedMsg{provider: id} })
		}
	}
	return s.List.Update(msg)
}

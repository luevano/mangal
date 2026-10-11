package watches

import (
	"context"
	"slices"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/util"
)

var _ base.State = (*historyState)(nil)

// historyState is every check of a provider's watches (or of every provider), latest
// first.
type historyState struct {
	list.Screen
	manager watch.Manager
	// provider is the one shown, empty for all
	provider string
	// providers are the ones to cycle through: the watches' and the checked ones
	providers []string
	// shown is the provider of the items, for a refresh to keep the selection
	shown *string

	open, next, refresh, library key.Binding
}

type historyMsg struct {
	provider  string
	providers []string
	items     []_list.Item
}

func newHistoryState(m watch.Manager, provider string) *historyState {
	s := &historyState{
		manager:  m,
		provider: provider,
		open:     util.Bind("details", "enter"),
		next:     util.Bind("next provider", "tab"),
		refresh:  util.Bind("refresh", "r"),
		library:  util.Bind("library manga", "L"),
	}
	s.Screen = list.NewScreen(list.New(2, 1, "check", "checks", []*runItem{}, func(i *runItem) _list.DefaultItem { return i }), s.keyMap)
	return s
}

func (s *historyState) keyMap() help.KeyMap {
	return util.KeyMap{Short: []key.Binding{s.open, s.next, s.library, s.refresh}}
}

func (s *historyState) Title() base.Title { return base.Title{Text: "History"} }

func (s *historyState) Subtitle() string {
	return providerName(s.provider) + " · " + s.List.Subtitle()
}

func providerName(p string) string {
	if p == "" {
		return "every provider"
	}
	return p
}

func (s *historyState) Init(ctx context.Context) tea.Cmd { return s.reload(ctx, "Loading the history") }

func (s *historyState) reload(ctx context.Context, label string) tea.Cmd {
	m, provider := s.manager, s.provider
	return base.Load(ctx, label, func(ctx context.Context) (historyMsg, error) {
		return loadHistory(ctx, m, provider)
	}, func(msg historyMsg) tea.Msg { return msg })
}

func loadHistory(ctx context.Context, m watch.Manager, provider string) (historyMsg, error) {
	runs, err := m.Runs(ctx, watch.RunFilter{Provider: provider, Limit: maxRuns})
	if err != nil {
		return historyMsg{}, err
	}
	watches, err := m.List(ctx)
	if err != nil {
		return historyMsg{}, err
	}
	return historyMsg{provider: provider, providers: providersOf(watches, runs), items: providerHistory(provider, runs)}, nil
}

// providersOf are the providers of the watches and of the checks, sorted.
func providersOf(watches []watch.Watch, runs []watch.CheckRun) []string {
	var providers []string
	for _, w := range watches {
		providers = append(providers, w.Provider)
	}
	for _, run := range runs {
		for _, r := range run.Results {
			providers = append(providers, r.Provider)
		}
	}
	slices.Sort(providers)
	return slices.Compact(providers)
}

// providerHistory are the checks of the provider's mangas (every one's when empty) in the
// runs, latest first.
func providerHistory(provider string, runs []watch.CheckRun) []_list.Item {
	var items []_list.Item
	for _, run := range runs {
		for _, r := range slices.Backward(run.Results) {
			if provider == "" || r.Provider == provider {
				items = append(items, &runItem{r: r, kind: run.Kind, watchTitle: true, started: true})
			}
		}
	}
	return items
}

// nextProvider is the provider after p in the cycle: every provider, then each one.
func nextProvider(providers []string, p string) string {
	i := slices.Index(providers, p)
	if i+1 >= len(providers) {
		return ""
	}
	return providers[i+1]
}

func (s *historyState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case historyMsg:
		// a reload of another provider, the cycle went on
		if msg.provider != s.provider {
			return nil
		}
		s.providers = msg.providers
		cmd := s.List.SetItems(msg.items)
		if s.shown == nil || *s.shown != msg.provider {
			s.List.Select(0)
		}
		s.shown = &msg.provider
		return cmd
	case tea.KeyMsg:
		if s.List.Filtering() {
			break
		}
		switch {
		case key.Matches(msg, s.refresh):
			return s.reload(ctx, "Refreshing")
		case key.Matches(msg, s.next):
			s.provider = nextProvider(s.providers, s.provider)
			return s.reload(ctx, "Loading the history of "+providerName(s.provider))
		}
		// no provider history key, it's this screen
		if cmd, ok := runItemKey(ctx, msg, s.List, s.manager, s.open, s.library, key.Binding{}); ok {
			return cmd
		}
	}
	return s.List.Update(msg)
}

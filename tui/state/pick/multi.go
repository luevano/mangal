package pick

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	_list "charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/model/search"
	"github.com/luevano/mangal/v5/tui/model/searchlist"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/luevano/mangal/v5/util/cache"
)

// Search searches several providers at once (core.Core.Search). Swapped in tests.
var Search = func(ctx context.Context, req core.SearchReq) <-chan core.SearchResult {
	c, err := base.Core()
	if err != nil {
		results := make(chan core.SearchResult, len(req.Providers))
		for _, p := range req.Providers {
			results <- core.SearchResult{Provider: p, Err: err}
		}
		close(results)
		return results
	}
	return c.Search(ctx, req)
}

// MultiOptions say what a Multi screen searches and what enter does.
type MultiOptions struct {
	Title     string
	Providers []libmangal.ProviderInfo
	Query     string
	// Action is the help of enter.
	Action string
	// Pick is what enter does with the selected manga of the provider.
	Pick func(ctx context.Context, provider string, manga mangadata.Manga) tea.Cmd
}

var _ base.State = (*Multi)(nil)

// Multi searches several providers at once: a section per provider, filled as each one
// answers; enter picks a manga (MultiOptions.Pick).
type Multi struct {
	searchlist.Screen
	opts     MultiOptions
	query    string
	sections []*sectionItem
	// cancel ends the running search
	cancel context.CancelFunc
	frame  int
	// spinGen numbers the spinner's ticks, a restarted one drops the older
	spinGen int

	Assign, searchKey key.Binding
}

// sectionItem is a provider's section: searching, its error, or its results.
type sectionItem struct {
	info   libmangal.ProviderInfo
	done   bool
	err    error
	mangas []*MultiItem
	frame  *int
}

func (i *sectionItem) ItemKey() string     { return "provider " + i.info.ID }
func (i *sectionItem) FilterValue() string { return i.info.Name }
func (i *sectionItem) Title() string       { return style.Bold.Accent.Render("── " + i.info.Name) }
func (i *sectionItem) Description() string {
	switch {
	case !i.done:
		frames := base.DotSpinner.Frames
		return style.Bold.Accent.Render(frames[*i.frame%len(frames)]) + style.Normal.Secondary.Render(" searching…")
	case i.err != nil:
		return style.Normal.Error.Render(i.err.Error())
	case len(i.mangas) == 1:
		return style.Normal.Secondary.Render("1 manga")
	}
	return style.Normal.Secondary.Render(fmt.Sprintf("%d mangas", len(i.mangas)))
}

// MultiItem is a provider's search result.
type MultiItem struct {
	Provider string
	Manga    mangadata.Manga
}

func (i *MultiItem) ItemKey() string     { return i.Provider + " " + i.Manga.Info().ID }
func (i *MultiItem) FilterValue() string { return i.Manga.Info().Title }
func (i *MultiItem) Title() string       { return "  " + i.Manga.Info().Title }
func (i *MultiItem) Description() string { return "  " + i.Manga.Info().URL }

// multiResultMsg is a provider's answer to the search number n, next reads the one after.
type multiResultMsg struct {
	search int
	result core.SearchResult
	next   tea.Cmd
}

type multiSpinMsg struct {
	multi *Multi
	gen   int
}

const multiSpinEvery = 100 * time.Millisecond

// NewMulti is the search of the providers, of opts.Query when it's set.
func NewMulti(opts MultiOptions) *Multi {
	l := list.New(2, 1, "item", "items", []_list.Item{}, func(i _list.Item) _list.DefaultItem { return i.(_list.DefaultItem) })
	s := &Multi{
		Screen: searchlist.New(search.New("Search the manga...", opts.Query, 64, 5), l, searchlist.History{
			Get: cache.GetMangasSearchHistory,
			Set: cache.SetMangasSearchHistory,
		}),
		opts:      opts,
		Assign:    util.Bind(opts.Action, "enter"),
		searchKey: base.KeySearch,
	}
	s.updateKeys()
	return s
}

// Keys are the search's own keys, without the list's.
func (s *Multi) Keys() help.KeyMap {
	return util.KeyMap{Short: []key.Binding{s.Assign, s.searchKey}}
}

func (s *Multi) KeyMap() help.KeyMap {
	return base.CombinedKeyMap(s.Keys(), s.ListKeyMap())
}

func (s *Multi) Title() base.Title { return base.Title{Text: s.opts.Title} }

func (s *Multi) Subtitle() string {
	names := make([]string, len(s.opts.Providers))
	for i, p := range s.opts.Providers {
		names[i] = p.Name
	}
	return "on " + strings.Join(names, ", ")
}

func (s *Multi) Init(ctx context.Context) tea.Cmd {
	if s.Search.Query() == "" {
		return tea.Batch(s.Screen.Init(ctx), s.Search.Focus())
	}
	return tea.Batch(s.Screen.Init(ctx), s.searchCmd(ctx, s.Search.Query()))
}

// Destroy ends the running search.
func (s *Multi) Destroy() {
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *Multi) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case multiResultMsg:
		if !s.Latest(msg.search) {
			return nil
		}
		s.setResult(msg.result)
		return tea.Batch(s.setItems(false), msg.next)
	case base.RestoredMsg:
		return tea.Batch(s.resumeCmd(ctx), s.Screen.Update(msg))
	case multiSpinMsg:
		if msg.multi != s || msg.gen != s.spinGen || !s.searching() {
			return nil
		}
		s.frame++
		return s.tick()
	case search.SearchMsg:
		return tea.Batch(s.Screen.Update(msg), s.searchCmd(ctx, string(msg)))
	case tea.KeyPressMsg:
		if s.Inputting() {
			break
		}
		switch {
		case key.Matches(msg, s.Assign):
			i, ok := s.List.SelectedItem().(*MultiItem)
			if !ok || base.Busy(ctx) {
				return nil
			}
			return s.opts.Pick(ctx, i.Provider, i.Manga)
		case key.Matches(msg, s.searchKey):
			s.List.ResetFilter()
			return s.Search.Focus()
		}
	}
	return s.Screen.Update(msg)
}

// searchCmd searches the query on every provider.
func (s *Multi) searchCmd(ctx context.Context, query string) tea.Cmd {
	s.query = query
	s.sections = make([]*sectionItem, len(s.opts.Providers))
	for i, p := range s.opts.Providers {
		s.sections[i] = &sectionItem{info: p, frame: &s.frame}
	}
	return tea.Batch(s.setItems(true), s.start(ctx, s.sections))
}

// resumeCmd searches again on the providers whose search was cut short: the screen's ctx
// ends when another is shown over it.
func (s *Multi) resumeCmd(ctx context.Context) tea.Cmd {
	var cut []*sectionItem
	for _, sec := range s.sections {
		if !sec.done || errors.Is(sec.err, context.Canceled) {
			sec.done, sec.err, sec.mangas = false, nil, nil
			cut = append(cut, sec)
		}
	}
	if len(cut) == 0 {
		return nil
	}
	return tea.Batch(s.setItems(false), s.start(ctx, cut))
}

// start searches the query on the sections' providers, the results come one at a time.
func (s *Multi) start(ctx context.Context, sections []*sectionItem) tea.Cmd {
	if s.cancel != nil {
		s.cancel()
	}
	n := s.NewSearch()
	ctx, s.cancel = context.WithCancel(ctx)
	ids := make([]string, len(sections))
	for i, sec := range sections {
		ids[i] = sec.info.ID
	}
	results := Search(ctx, core.SearchReq{Providers: ids, Query: s.query})
	var next tea.Cmd
	next = func() tea.Msg {
		r, ok := <-results
		if !ok {
			return nil
		}
		return multiResultMsg{search: n, result: r, next: next}
	}
	return tea.Batch(next, s.spin())
}

func (s *Multi) spin() tea.Cmd {
	s.spinGen++
	return s.tick()
}

func (s *Multi) tick() tea.Cmd {
	gen := s.spinGen
	return tea.Tick(multiSpinEvery, func(time.Time) tea.Msg { return multiSpinMsg{multi: s, gen: gen} })
}

func (s *Multi) searching() bool {
	for _, sec := range s.sections {
		if !sec.done {
			return true
		}
	}
	return false
}

func (s *Multi) setResult(r core.SearchResult) {
	for _, sec := range s.sections {
		if sec.info.ID != r.Provider || sec.done {
			continue
		}
		sec.done, sec.err = true, r.Err
		for _, m := range r.Mangas {
			sec.mangas = append(sec.mangas, &MultiItem{Provider: r.Provider, Manga: m})
		}
		return
	}
}

// setItems shows the sections, each with its mangas: those of a new search, else the
// same one's keeping the selected item.
func (s *Multi) setItems(fresh bool) tea.Cmd {
	var items []_list.Item
	for _, sec := range s.sections {
		items = append(items, sec)
		for _, m := range sec.mangas {
			items = append(items, m)
		}
	}
	var cmd tea.Cmd
	if fresh {
		cmd = s.SetResults(items)
	} else {
		cmd = s.List.SetItemsKeep(items)
	}
	s.updateKeys()
	return cmd
}

func (s *Multi) updateKeys() {
	s.Assign.SetEnabled(len(s.List.Items()) > len(s.sections))
}

package mangas

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	clientanilist "github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/model/metadata"
	"github.com/luevano/mangal/v5/tui/model/search"
	"github.com/luevano/mangal/v5/tui/model/searchlist"
	"github.com/luevano/mangal/v5/tui/state/anilist"
	"github.com/luevano/mangal/v5/tui/state/chapters"
	"github.com/luevano/mangal/v5/tui/state/volumes"
	"github.com/luevano/mangal/v5/tui/state/watches"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/luevano/mangal/v5/util/cache"
)

var _ base.State = (*state)(nil)

type state struct {
	searchlist.Screen
	client *libmangal.Client

	// the items point to these
	extraInfo     bool
	fullExtraInfo bool
	// watched are the watched mangas, for the marks
	watched watches.Set

	keys keyMap
}

type keyMap struct {
	confirm,
	search,
	anilist,
	metadata,
	info,
	watch,
	jump,
	toggleFullMeta key.Binding
}

type resultsMsg []_list.Item

// openedMsg is a manga ready to be shown: its metadata (nil if none was searched or
// found) and its volumes.
type openedMsg struct {
	item    *item
	meta    lmmeta.Metadata
	notice  string
	volumes []mangadata.Volume
	// chapters of every volume, when they're shown right away
	chapters []mangadata.Chapter
	expanded string
	// volume is the single volume shown right away
	volume mangadata.Volume
}

func New(client *libmangal.Client) base.State {
	s := &state{
		client: client,
		keys: keyMap{
			confirm:        util.Bind("confirm", "enter"),
			search:         base.KeySearch,
			anilist:        base.KeyBindings,
			metadata:       base.KeyMetadata,
			info:           base.KeyInfo,
			watch:          base.KeyWatch,
			jump:           base.KeyItsWatch,
			toggleFullMeta: util.Bind("toggle full meta", "M"),
		},
	}
	l := list.New(2, 1, "manga", "mangas", []mangadata.Manga{}, func(manga mangadata.Manga) _list.DefaultItem {
		return newItem(manga, &s.extraInfo, &s.fullExtraInfo)
	})
	s.Screen = searchlist.New(search.New("Search manga...", "", 64, 5), l, searchlist.History{
		Get: cache.GetMangasSearchHistory,
		Set: cache.SetMangasSearchHistory,
	})
	s.updateKeys()
	return s
}

func (s *state) KeyMap() help.KeyMap {
	k := s.keys
	return base.CombinedKeyMap(util.KeyMap{
		Short: []key.Binding{k.confirm, k.search, k.anilist, k.metadata, k.watch},
		Full:  [][]key.Binding{{k.confirm, k.search, k.info, k.watch, k.jump}, {k.anilist, k.metadata, k.toggleFullMeta}},
	}, s.ListKeyMap())
}

func (s *state) Title() base.Title {
	return base.Title{Text: s.client.Info().Name}
}

func (s *state) Subtitle() string {
	if !s.Searched() {
		return "Search on " + s.client.Info().Name
	}
	return s.Screen.Subtitle()
}

func (s *state) Init(ctx context.Context) tea.Cmd {
	return tea.Batch(s.Screen.Init(ctx), s.Search.Focus(), loginCmd(ctx), watchedCmd(ctx))
}

type watchedMsg watches.Set

func watchedCmd(ctx context.Context) tea.Cmd {
	return base.Load(ctx, "", watches.Watched, func(set watches.Set) tea.Msg { return watchedMsg(set) })
}

// markWatched marks the watched items.
func (s *state) markWatched() {
	id := s.client.Info().ID
	for _, i := range s.List.Items() {
		i := i.(*item)
		i.watched = s.watched.Has(id, i.manga.Info().ID)
	}
}

// loginCmd logs into Anilist while the user types, for the metadata search. A cancelled
// login is tried again by the next Anilist request.
func loginCmd(ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		release, ok := base.Track(ctx)
		if !ok {
			return nil
		}
		defer release()
		_ = clientanilist.EnsureLogin(ctx)
		return nil
	}
}

func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case resultsMsg:
		cmd := s.SetResults(msg)
		s.markWatched()
		s.updateKeys()
		return cmd
	case watchedMsg:
		s.watched = watches.Set(msg)
		s.markWatched()
		return nil
	case openedMsg:
		return s.onOpened(msg)
	case search.SearchMsg:
		return tea.Batch(s.Screen.Update(msg), s.searchCmd(ctx, string(msg)))
	case search.SearchCancelMsg:
		if s.Search.Query() == "" {
			return base.Back
		}
		return nil
	case base.RestoredMsg:
		// the metadata may have changed (Anilist screen)
		for _, i := range s.List.Items() {
			i.(*item).renderMetadata()
		}
		// e.g. watched with w
		return watchedCmd(ctx)
	case tea.KeyMsg:
		if s.Inputting() {
			break
		}
		if cmd, ok := s.updateKey(ctx, msg); ok {
			return cmd
		}
	}
	return s.Screen.Update(msg)
}

func (s *state) updateKey(ctx context.Context, msg tea.KeyMsg) (tea.Cmd, bool) {
	switch {
	case key.Matches(msg, s.keys.search):
		s.List.ResetFilter()
		return s.Search.Focus(), true
	case key.Matches(msg, s.keys.info):
		s.extraInfo = !s.extraInfo
		s.updateKeys()
		return nil, true
	case key.Matches(msg, s.keys.toggleFullMeta):
		s.fullExtraInfo = !s.fullExtraInfo
		return nil, true
	}

	i, ok := s.List.SelectedItem().(*item)
	if !ok {
		return nil, false
	}
	switch {
	case key.Matches(msg, s.keys.confirm):
		if base.Busy(ctx) {
			return nil, true
		}
		return s.openCmd(ctx, i), true
	case key.Matches(msg, s.keys.anilist):
		return anilist.Open(s.client, i.manga), true
	case key.Matches(msg, s.keys.metadata):
		return metadata.ShowManga(i.manga), true
	case key.Matches(msg, s.keys.watch):
		return func() tea.Msg { return watches.NewAdd(s.client, i.manga, nil) }, true
	case key.Matches(msg, s.keys.jump):
		w, ok := s.watched.Find(s.client.Info().ID, i.manga.Info().ID)
		if !ok {
			return watches.JumpCmd(nil, "w watches it"), true
		}
		return watches.JumpCmd([]watch.Watch{w}, ""), true
	}
	return nil, false
}

func (s *state) searchCmd(ctx context.Context, query string) tea.Cmd {
	return base.Load(ctx, fmt.Sprintf("Searching for %q", query), func(ctx context.Context) ([]_list.Item, error) {
		mangas, err := s.client.SearchMangasContext(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("search %q: %w", query, err)
		}
		items := make([]_list.Item, len(mangas))
		for i, m := range mangas {
			items[i] = newItem(m, &s.extraInfo, &s.fullExtraInfo)
		}
		return items, nil
	}, func(items []_list.Item) tea.Msg { return resultsMsg(items) })
}

// openCmd searches the manga's metadata (download.metadata.search) and its volumes, or
// right away the chapters (tui.expand_*).
func (s *state) openCmd(ctx context.Context, i *item) tea.Cmd {
	manga := i.manga
	// the metadata is read here, it's only changed in Update
	searchMeta := config.Download.Metadata.Search.Get() && !hasMetadata(manga.Metadata())
	// the config isn't read from the background (viper isn't safe for it)
	expandAll, expandSingle := config.TUI.ExpandAllVolumes.Get(), config.TUI.ExpandSingleVolume.Get()
	label := fmt.Sprintf("Searching volumes for %q", manga)
	if searchMeta {
		label = fmt.Sprintf("Searching metadata and volumes for %q", manga)
	}
	return base.Load(ctx, label, func(ctx context.Context) (openedMsg, error) {
		msg := openedMsg{item: i}
		if searchMeta {
			msg.meta, msg.notice = s.searchMetadata(ctx, manga)
		}

		vols, err := s.client.MangaVolumesContext(ctx, manga)
		if err != nil {
			return msg, fmt.Errorf("get the volumes of %q: %w", manga, err)
		}
		msg.volumes = vols

		var expand []mangadata.Volume
		switch {
		case expandAll:
			expand, msg.expanded = vols, fmt.Sprintf("Skipped selecting volumes (cfg: %s)", config.TUI.ExpandAllVolumes.Key)
		case len(vols) == 1 && expandSingle:
			expand, msg.expanded = vols, fmt.Sprintf("Skipped single volume (cfg: %s)", config.TUI.ExpandSingleVolume.Key)
			msg.volume = vols[0]
		}
		for _, v := range expand {
			c, err := s.client.VolumeChaptersContext(ctx, v)
			if err != nil {
				return msg, fmt.Errorf("get the chapters of volume %s: %w", v, err)
			}
			msg.chapters = append(msg.chapters, c...)
		}
		return msg, nil
	}, func(msg openedMsg) tea.Msg { return msg })
}

// searchMetadata is the manga's default metadata binding, else it finds the manga on Anilist,
// or Kitsu. Without a match the manga still opens, a strict download then says what to do.
func (s *state) searchMetadata(ctx context.Context, manga mangadata.Manga) (lmmeta.Metadata, string) {
	meta, err := client.SearchMetadata(ctx, s.client, manga)
	switch {
	case err != nil:
		log.Warn("search metadata for %q: %s", manga, err)
		return nil, fmt.Sprintf("Metadata search failed: %s", err)
	case meta == nil:
		log.Info("no metadata found for %q", manga)
		return nil, fmt.Sprintf("No metadata match for %q", manga)
	}
	if err := lmmeta.Validate(meta); err != nil {
		log.Info("metadata %q for %q: %s", meta, manga, err)
		return nil, fmt.Sprintf("No usable metadata match for %q: %s", manga, err)
	}
	log.Info("found metadata for %q: %q", manga, meta)
	return meta, ""
}

func (s *state) onOpened(msg openedMsg) tea.Cmd {
	manga := msg.item.manga
	if msg.meta != nil {
		manga.SetMetadata(msg.meta)
		msg.item.renderMetadata()
	}

	var next base.State
	if msg.expanded != "" {
		next = chapters.New(s.client, manga, msg.volume, msg.chapters)
	} else {
		next = volumes.New(s.client, manga, msg.volumes)
	}

	cmds := []tea.Cmd{func() tea.Msg { return next }}
	notice := msg.notice
	if msg.expanded != "" {
		notice = joinNotice(notice, msg.expanded)
	}
	if notice != "" {
		cmds = append(cmds, base.NotifyWithDuration(notice, 3*time.Second))
	}
	return tea.Sequence(cmds...)
}

func joinNotice(a, b string) string {
	if a == "" {
		return b
	}
	return a + " · " + b
}

// hasMetadata is true when the manga's metadata is from a metadata provider (bound or found
// before), not the provider's.
func hasMetadata(meta lmmeta.Metadata) bool {
	if meta == nil || lmmeta.Validate(meta) != nil {
		return false
	}
	source := meta.ID().Source
	return source == lmmeta.IDSourceAnilist || source == lmmeta.IDSourceKitsu || source == lmmeta.IDSourceMyAnimeList
}

func (s *state) updateKeys() {
	enable := len(s.List.Items()) != 0
	s.keys.confirm.SetEnabled(enable)
	s.keys.anilist.SetEnabled(enable)
	s.keys.metadata.SetEnabled(enable)
	s.keys.watch.SetEnabled(enable)
	s.keys.jump.SetEnabled(enable)
	s.keys.toggleFullMeta.SetEnabled(enable && s.extraInfo)
}

package library

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	_list "charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/state/watches"
	"github.com/luevano/mangal/v5/tui/util"
	stringutil "github.com/luevano/mangal/v5/util/string"
	"github.com/skratchdot/open-golang/open"
)

var _ base.State = (*mangaState)(nil)

type downloadItem struct {
	download storage.Download
}

func (i *downloadItem) FilterValue() string { return i.download.ChapterTitle }
func (i *downloadItem) ItemKey() string     { return i.download.Path }

func (i *downloadItem) Title() string {
	title := stringutil.FormatFloat32(i.download.Chapter)
	if i.download.ChapterTitle != "" {
		title += " - " + i.download.ChapterTitle
	}
	return title
}

func (i *downloadItem) Description() string {
	desc := fmt.Sprintf("%s · %s", i.download.Format, i.download.Provider)
	if i.download.Pages != 0 {
		desc += fmt.Sprintf(" · %d pages", i.download.Pages)
	}
	return desc + " · " + i.download.Path
}

type mangaState struct {
	list.Screen
	manga storage.Manga
	// watches and bindings are the manga's, for the header
	watches  []watch.Watch
	bindings []storage.Binding
	size     base.Size

	open, openURL, scan, delete, watch, itsWatch, links, hold, retitle key.Binding
}

type downloadsMsg []_list.Item

type mangaWatchesMsg []watch.Watch

type mangaBindingsMsg []storage.Binding

// mangaScannedMsg is the result of a scan, and the manga after it.
type mangaScannedMsg struct {
	result library.ScanResult
	manga  storage.Manga
}

func newMangaState(manga storage.Manga) *mangaState {
	s := &mangaState{
		manga:    manga,
		open:     util.Bind("open", "enter"),
		openURL:  base.KeyOpenURL,
		scan:     base.KeyScan,
		delete:   base.KeyDelete,
		watch:    base.KeyWatch,
		itsWatch: base.KeyItsWatch,
		links:    base.KeyBindings,
		hold:     base.KeyHold,
		retitle:  base.KeyRetitle,
	}
	s.Screen = list.NewScreen(list.New(2, 1, "chapter", "chapters", []*downloadItem{}, func(i *downloadItem) _list.DefaultItem { return i }), s.keyMap)
	s.Confirm = newDialog()
	return s
}

func (s *mangaState) keyMap() help.KeyMap {
	return util.KeyMap{Short: []key.Binding{s.open, s.openURL, s.scan, s.delete, s.watch, s.itsWatch, s.links, s.hold, s.retitle}}
}

// watchesCmd loads the manga's watches.
func (s *mangaState) watchesCmd(ctx context.Context) tea.Cmd {
	manga := s.manga
	return base.Load(ctx, "", watches.Watched, func(set watches.Set) tea.Msg { return mangaWatchesMsg(set.ForLibrary(manga)) })
}

type mangaMsg storage.Manga

// mangaCmd loads the manga again (its title or folder may have changed).
func (s *mangaState) mangaCmd(ctx context.Context) tea.Cmd {
	id := s.manga.ID
	return base.Load(ctx, "", func(ctx context.Context) (mangaMsg, error) {
		lib, err := base.Library()
		if err != nil {
			return mangaMsg{}, err
		}
		m, err := lib.Manga(ctx, id)
		if errors.Is(err, core.ErrNotFound) {
			return mangaMsg{}, nil
		}
		return mangaMsg(m), err
	}, func(m mangaMsg) tea.Msg {
		if m.ID == 0 {
			return nil
		}
		return m
	})
}

// bindingsCmd loads the manga's metadata bindings.
func (s *mangaState) bindingsCmd(ctx context.Context) tea.Cmd {
	id := s.manga.ID
	return base.Load(ctx, "", func(ctx context.Context) (mangaBindingsMsg, error) {
		lib, err := base.Library()
		if err != nil {
			return nil, err
		}
		return lib.MangaBindings(ctx, id)
	}, func(b mangaBindingsMsg) tea.Msg { return b })
}

// header is the manga's sources, watch state and metadata bindings, empty when it has
// none.
func (s *mangaState) header() string {
	lines := library.SourceLines(s.manga.Sources)
	if s.manga.Hold != "" {
		lines = append(lines, library.HoldLabel(s.manga.Hold)+" ("+s.hold.Help().Key+" changes it)")
	}
	if s.manga.NewProvider != "" {
		lines = append(lines, "needs a new provider: "+s.manga.NewProvider)
	}
	for _, w := range s.watches {
		lines = append(lines, watches.Describe(w))
	}
	b := formatBindings(s.bindings)
	if status := library.StatusLabel(s.manga.Status); status != "" {
		b = strings.TrimPrefix(b+" · "+status, " · ")
	}
	if b != "" {
		lines = append(lines, "metadata: "+b)
	}
	return strings.Join(lines, "\n")
}

// formatBindings are the bindings of a library manga's sources as "al 132029 (default),
// mal 30230", each source's prefixed by its provider when there are several, and the
// library manga's own ids (its links) none has.
func formatBindings(bindings []storage.Binding) string {
	type group struct {
		name string
		list []string
	}
	var groups []*group
	byName := map[string]*group{}
	for _, b := range bindings {
		name := b.Provider
		if name == "" {
			name = "library"
		}
		g, ok := byName[name]
		if !ok {
			g = &group{name: name}
			byName[name] = g
			groups = append(groups, g)
		}
		entry := fmt.Sprintf("%s %s", b.Source, b.ID)
		if b.Default {
			entry += " (default)"
		}
		g.list = append(g.list, entry)
	}
	if len(groups) == 1 && groups[0].name != "library" {
		return strings.Join(groups[0].list, ", ")
	}
	parts := make([]string, len(groups))
	for i, g := range groups {
		parts[i] = g.name + ": " + strings.Join(g.list, ", ")
	}
	return strings.Join(parts, "; ")
}

func (s *mangaState) reload(ctx context.Context) tea.Cmd {
	id := s.manga.ID
	return base.Load(ctx, "", func(ctx context.Context) (downloadsMsg, error) {
		lib, err := base.Library()
		if err != nil {
			return nil, err
		}
		downloads, err := lib.Downloads(ctx, id)
		if err != nil {
			return nil, err
		}
		items := make([]_list.Item, len(downloads))
		for i, dl := range downloads {
			items[i] = &downloadItem{download: dl}
		}
		return items, nil
	}, func(items downloadsMsg) tea.Msg { return items })
}

// scanCmd reconciles this manga with its folder, the list then shows what's really there.
func (s *mangaState) scanCmd(ctx context.Context) tea.Cmd {
	id := s.manga.ID
	return base.LoadWithStatus(ctx, "", func(ctx context.Context, status func(string)) (mangaScannedMsg, error) {
		lib, err := base.Library()
		if err != nil {
			return mangaScannedMsg{}, err
		}
		scan, err := lib.ScanManga(base.WithStatus(ctx, status), id)
		if err != nil {
			return mangaScannedMsg{}, err
		}
		return mangaScannedMsg{result: scan.Result, manga: scan.Manga}, nil
	}, func(msg mangaScannedMsg) tea.Msg { return msg })
}

func (s *mangaState) onScanned(ctx context.Context, msg mangaScannedMsg) tea.Cmd {
	s.manga = msg.manga
	res := msg.result
	resize := s.Resize(s.size)
	switch {
	case len(res.Missing) != 0:
		return tea.Batch(resize, withNotify(s.reload(ctx), "the manga's folder wasn't found, a library scan looks everywhere"))
	case res.Added+res.Moved+res.Removed+res.Relocated != 0:
		return tea.Batch(resize, withNotify(s.reload(ctx), fmt.Sprintf("updated from the files: %d added, %d moved, %d removed", res.Added, res.Moved, res.Removed)))
	}
	return tea.Batch(resize, s.reload(ctx))
}

func (s *mangaState) Key() string       { return fmt.Sprintf("library/%d", s.manga.ID) }
func (s *mangaState) Title() base.Title { return base.Title{Text: s.manga.Title} }
func (s *mangaState) Subtitle() string  { return s.manga.Path() }
func (s *mangaState) Resize(size base.Size) tea.Cmd {
	s.size = size
	s.Confirm.Resize(size)
	if h := s.header(); h != "" {
		size.Height = max(1, size.Height-lipgloss.Height(h)-1)
	}
	return s.List.Resize(size)
}

func (s *mangaState) Init(ctx context.Context) tea.Cmd {
	return tea.Batch(s.scanCmd(ctx), s.watchesCmd(ctx), s.bindingsCmd(ctx))
}

func (s *mangaState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	if cmd, ok := s.Answer(msg); ok {
		return cmd
	}
	switch msg := msg.(type) {
	case downloadsMsg:
		return s.List.SetItemsKeep(msg)
	case mangaWatchesMsg:
		s.watches = msg
		return s.Resize(s.size)
	case mangaBindingsMsg:
		s.bindings = msg
		return s.Resize(s.size)
	case base.RestoredMsg:
		// e.g. paused or bound on its watch's screen, linked (maybe renamed) on its links',
		// chapters downloaded by a watch meanwhile
		return tea.Batch(s.watchesCmd(ctx), s.bindingsCmd(ctx), s.mangaCmd(ctx), s.reload(ctx))
	case mangaMsg:
		// its sources may have changed, the header with them
		s.manga = storage.Manga(msg)
		return s.Resize(s.size)
	case mangaScannedMsg:
		return s.onScanned(ctx, msg)
	case actionMsg:
		return msg.run(ctx)
	case reloadMsg:
		return withNotify(s.reload(ctx), msg.notify)
	case tea.KeyPressMsg:
		if s.List.Filtering() {
			break
		}
		switch {
		case key.Matches(msg, s.openURL):
			return s.openURLCmd(ctx)
		case key.Matches(msg, s.itsWatch):
			return watches.JumpCmd(s.watches, s.watch.Help().Key+" watches it")
		case key.Matches(msg, s.watch):
			return s.addWatch(ctx)
		case key.Matches(msg, s.links):
			return linksCmd(s.manga)
		case key.Matches(msg, s.retitle):
			return retitleCmd(s.manga)
		case key.Matches(msg, s.hold):
			holdCmd(s.Confirm, s.View(), s.manga, s.watches)
			return nil
		case key.Matches(msg, s.scan):
			if base.Busy(ctx) {
				return nil
			}
			return s.scanCmd(ctx)
		case key.Matches(msg, s.delete):
			item, ok := s.List.SelectedItem().(*downloadItem)
			if !ok {
				break
			}
			if base.Busy(ctx) {
				return base.Notify(busyNotice)
			}
			dl, title := item.download, item.Title()
			message := fmt.Sprintf("Delete chapter %s (%s)? The file goes to the trash.", title, dl.Format)
			s.Confirm.AskDelete("Delete", message, s.View(), change("Deleting chapter "+title, core.DeleteReq{Manga: s.manga.ID, Chapter: dl.Path}, fmt.Sprintf("deleted chapter %s (in the trash until emptied)", title)))
			return nil
		case key.Matches(msg, s.open):
			if item, ok := s.List.SelectedItem().(*downloadItem); ok {
				p := item.download.Path
				return func() tea.Msg {
					if err := open.Start(p); err != nil {
						return fmt.Errorf("open %s: %w", p, err)
					}
					return nil
				}
			}
		}
	}
	return s.List.Update(msg)
}

// openURLCmd opens the web page of the manga's current source, looked up (and recorded)
// when it isn't known.
func (s *mangaState) openURLCmd(ctx context.Context) tea.Cmd {
	if len(s.manga.Sources) == 0 {
		return base.Notify("No source, no web page")
	}
	id, src := s.manga.ID, s.manga.Sources[0]
	label := ""
	if !core.WebURL(src.URL) || !src.Gone.IsZero() {
		label = "Looking up the address on " + src.Provider
	}
	return base.Load(ctx, label, func(ctx context.Context) (string, error) {
		lib, err := base.Library()
		if err != nil {
			return "", err
		}
		u, err := lib.SourceURL(ctx, id, src.Provider, src.MangaID, false)
		if err != nil {
			return "", err
		}
		if err := openURL(u); err != nil {
			return "", fmt.Errorf("open %s: %w", u, err)
		}
		return u, nil
	}, func(string) tea.Msg { return nil })
}

// openURL opens a web page in the browser. Swapped in tests.
var openURL = open.Start

// addWatch watches the manga on its current source (the one its folder follows), with
// the watch screen's options; it opens its watch when it's watched there already.
func (s *mangaState) addWatch(ctx context.Context) tea.Cmd {
	if len(s.manga.Sources) == 0 {
		return base.Notify("No source to watch it on")
	}
	src := s.manga.Sources[0]
	for _, w := range s.watches {
		if w.Provider == src.Provider && w.MangaID == src.MangaID {
			return func() tea.Msg { return watches.NewDetails(w) }
		}
	}
	if base.Busy(ctx) {
		return nil
	}
	title := src.Title
	if title == "" {
		title = s.manga.Title
	}
	return watches.AddCmd(ctx, src.Provider, src.MangaID, title)
}

func (s *mangaState) View() string {
	if s.Asking() {
		return s.Confirm.View()
	}
	if h := s.header(); h != "" {
		return h + "\n\n" + s.List.View()
	}
	return s.List.View()
}

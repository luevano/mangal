// Package chapters is the chapters of a manga (or a volume): download, read, toggle.
package chapters

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/confirm"
	"github.com/luevano/mangal/v5/tui/model/format"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/model/metadata"
	"github.com/luevano/mangal/v5/tui/state/anilist"
	"github.com/luevano/mangal/v5/tui/state/watches"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/luevano/mangal/v5/util/afs"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/zyedidia/generic/set"
)

var _ base.State = (*state)(nil)

type state struct {
	list.Screen
	confirm *confirm.Dialog
	formats *format.Model
	// inFormats shows the format picker over formatsBehind
	inFormats     bool
	formatsBehind string

	volume mangadata.Volume // nil when the chapters are the manga's
	manga  mangadata.Manga
	client *libmangal.Client

	selected set.Set[*item]
	// show is what the items show, they point to it
	show show
	// fullMeta shows the metadata's source and id in the status
	fullMeta bool
	// watched is the manga's watch, empty when it isn't
	watched []watch.Watch

	renderedSep,
	renderedSubtitleFormats string

	size   base.Size
	styles styles
	keys   keyMap
}

type keyMap struct {
	toggle,
	read,
	download,
	info,
	anilist,
	metadata,
	changeFormat,
	openURL,
	watch,
	jump,
	selectAll,
	unselectAll,
	toggleVolumeNumber,
	toggleChapterNumber,
	toggleGroup,
	toggleDate key.Binding
}

// filesMsg is where the chapters are downloaded: in the library by number and format,
// and in the temp dir (aligned with items); and which were read, by number.
type filesMsg struct {
	downloads map[float32]map[libmangal.Format]string
	read      map[float32]bool
	items     []*item
	temp      []map[libmangal.Format]string
}

// New is the chapters of the volume, nil for chapters of the whole manga.
func New(client *libmangal.Client, manga mangadata.Manga, volume mangadata.Volume, chapters []mangadata.Chapter) base.State {
	_styles := defaultStyles()
	s := &state{
		confirm: confirm.NewDialog(30, color.Success),
		formats: format.New(color.Viewport),
		volume:  volume,
		manga:   manga,
		client:  client,
		show: show{
			volumeNumber:  config.TUI.Chapter.ShowVolumeNumber.Get(),
			chapterNumber: config.TUI.Chapter.ShowNumber.Get(),
			group:         config.TUI.Chapter.ShowGroup.Get(),
			date:          config.TUI.Chapter.ShowDate.Get(),
		},
		selected:    set.NewMapset[*item](),
		renderedSep: _styles.sep.Render(icon.Separator.Raw()),
		styles:      _styles,
		keys: keyMap{
			toggle:              util.Bind("toggle", " "),
			read:                util.Bind("read", "r"),
			download:            util.Bind("download", "d"),
			info:                util.Bind("info", "i"),
			anilist:             util.Bind("metadata", "A"),
			metadata:            util.Bind("metadata", "m"),
			changeFormat:        util.Bind("change format", "f"),
			openURL:             util.Bind("open url", "o"),
			watch:               util.Bind("watch", "w"),
			jump:                util.Bind("its watch", "W"),
			selectAll:           util.Bind("select all", "a"),
			unselectAll:         util.Bind("unselect all", "backspace"),
			toggleVolumeNumber:  util.Bind("toggle vol num", "v"),
			toggleChapterNumber: util.Bind("toggle number", "c"),
			toggleGroup:         util.Bind("toggle group", "ctrl+g"),
			toggleDate:          util.Bind("toggle date", "ctrl+d"),
		},
	}

	volumeFormat, numberFormat := config.TUI.Chapter.VolumeNumberFormat.Get(), config.TUI.Chapter.NumberFormat.Get()
	s.Screen = list.NewScreen(list.New(3, 1, "chapter", "chapters", chapters, func(chapter mangadata.Chapter) _list.DefaultItem {
		return &item{
			chapter:               chapter,
			renderedSep:           s.renderedSep,
			renderedVolumeNumber:  fmt.Sprintf(volumeFormat, chapter.Volume()),
			renderedChapterNumber: fmt.Sprintf(numberFormat, chapter.Info().Number),
			show:                  &s.show,
			styles:                _styles,
		}
	}), s.keyMap)
	s.updateItemHeight()
	s.updateKeys()
	return s
}

func (s *state) keyMap() help.KeyMap {
	k := s.keys
	short := []key.Binding{k.toggle, k.read, k.download, k.info}
	return util.KeyMap{Short: short, Full: [][]key.Binding{
		short,
		{k.anilist, k.metadata, k.changeFormat, k.openURL, k.watch, k.jump},
		{k.selectAll, k.unselectAll},
		{k.toggleVolumeNumber, k.toggleChapterNumber, k.toggleGroup, k.toggleDate},
	}}
}

func (s *state) Backable() bool {
	return s.Screen.Backable() && !s.inFormats && !s.confirm.Active()
}

func (s *state) Inputting() bool {
	return s.Screen.Inputting() || s.inFormats || s.confirm.Active()
}

func (s *state) Title() base.Title {
	if s.volume != nil {
		return base.Title{Text: "Volume " + s.volume.String()}
	}
	return base.Title{Text: s.manga.String()}
}

func (s *state) Subtitle() string {
	var subtitle strings.Builder
	subtitle.Grow(100)
	subtitle.WriteString(s.List.Subtitle())
	if s.selected.Size() > 0 {
		subtitle.WriteString(s.renderedSep)
		subtitle.WriteString(s.styles.subtitle.Render(fmt.Sprintf("%d selected", s.selected.Size())))
	}
	subtitle.WriteString(s.renderedSubtitleFormats)
	if len(s.watched) != 0 {
		subtitle.WriteString(s.renderedSep)
		subtitle.WriteString(watches.Mark())
	}
	return subtitle.String()
}

func (s *state) Status() string {
	return metadata.Status(s.manga.Metadata(), s.fullMeta) + s.List.Status()
}

func (s *state) Resize(size base.Size) tea.Cmd {
	s.size = size
	s.confirm.Resize(size)
	return s.Screen.Resize(size)
}

func (s *state) Init(ctx context.Context) tea.Cmd {
	s.updateSubtitleFormats()
	return tea.Batch(s.formats.Init(), s.refresh(ctx, true), s.watchedCmd(ctx))
}

type watchedMsg []watch.Watch

func (s *state) watchedCmd(ctx context.Context) tea.Cmd {
	provider, id := s.client.Info().ID, s.manga.Info().ID
	return base.Load(ctx, "", watches.Watched, func(set watches.Set) tea.Msg {
		if w, ok := set.Find(provider, id); ok {
			return watchedMsg{w}
		}
		return watchedMsg(nil)
	})
}

func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return s.updateKey(ctx, msg)
	case filesMsg:
		s.setFiles(msg)
		return nil
	case watchedMsg:
		s.watched = msg
		return nil
	case downloadMsg:
		return s.download(ctx, msg)
	case downloadedMsg:
		return s.onDownloaded(ctx, msg)
	case readMsg:
		refresh := s.refresh(ctx, false)
		if msg.err != nil {
			return tea.Batch(refresh, func() tea.Msg { return msg.err })
		}
		return refresh
	case waitMsg:
		if msg.busy != "" {
			return tea.Batch(base.NotifyWithDuration(msg.busy, 10*time.Second), msg.next)
		}
		return tea.Batch(base.NotifyWithDuration(fmt.Sprintf("Rate limited, trying again in %s (#%d)", msg.wait, msg.attempt), msg.wait), msg.next)
	case format.BackMsg:
		s.inFormats = false
		s.updateSubtitleFormats()
		readFormat := config.Read.Format.Get()
		for _, i := range s.items() {
			i.setReadFormat(readFormat)
		}
		return nil
	case base.RestoredMsg:
		// e.g. after the download screen
		s.updateSubtitleFormats()
		return tea.Batch(s.refresh(ctx, false), s.watchedCmd(ctx))
	}
	return s.updateChild(msg)
}

// updateChild sends the msg to what's shown: the format picker or the list.
func (s *state) updateChild(msg tea.Msg) tea.Cmd {
	if s.inFormats {
		return s.formats.Update(msg)
	}
	return s.List.Update(msg)
}

func (s *state) updateKey(ctx context.Context, msg tea.KeyMsg) tea.Cmd {
	switch {
	case s.confirm.Active():
		return s.confirm.Update(msg)
	case s.inFormats, s.List.Filtering():
		return s.updateChild(msg)
	}

	k := s.keys
	switch {
	case key.Matches(msg, k.info):
		s.fullMeta = !s.fullMeta
	case key.Matches(msg, k.anilist):
		return anilist.Open(s.client, s.manga)
	case key.Matches(msg, k.metadata):
		return metadata.ShowManga(s.manga)
	case key.Matches(msg, k.watch):
		// a volume's chapters aren't all of them
		var chapters []mangadata.Chapter
		if s.volume == nil {
			for _, i := range s.items() {
				chapters = append(chapters, i.chapter)
			}
		}
		c, manga := s.client, s.manga
		return func() tea.Msg { return watches.NewAdd(c, manga, chapters) }
	case key.Matches(msg, k.jump):
		return watches.JumpCmd(s.watched, "w watches it")
	case key.Matches(msg, k.changeFormat):
		s.formatsBehind = s.View()
		s.inFormats = true
	case key.Matches(msg, k.toggleVolumeNumber):
		s.show.volumeNumber = !s.show.volumeNumber
	case key.Matches(msg, k.toggleChapterNumber):
		s.show.chapterNumber = !s.show.chapterNumber
	case key.Matches(msg, k.toggleGroup):
		s.show.group = !s.show.group
		s.updateItemHeight()
	case key.Matches(msg, k.toggleDate):
		s.show.date = !s.show.date
		s.updateItemHeight()
	case key.Matches(msg, k.selectAll):
		s.selectAll()
	case key.Matches(msg, k.unselectAll):
		for _, i := range s.selected.Keys() {
			i.toggle()
			s.selected.Remove(i)
		}
	default:
		return s.updateItemKey(ctx, msg)
	}
	return nil
}

// updateItemKey handles the keys on the hovered (or toggled) chapter.
func (s *state) updateItemKey(ctx context.Context, msg tea.KeyMsg) tea.Cmd {
	i, ok := s.List.SelectedItem().(*item)
	if !ok {
		return s.List.Update(msg)
	}
	switch {
	case key.Matches(msg, s.keys.toggle):
		if reason := i.unavailable(); reason != "" {
			return base.Notify("Chapter unavailable: " + reason)
		}
		i.toggle()
		if i.selected {
			s.selected.Put(i)
		} else {
			s.selected.Remove(i)
		}
		return nil
	case key.Matches(msg, s.keys.read):
		return s.askRead(ctx, i)
	case key.Matches(msg, s.keys.download):
		return s.askDownload(ctx, i)
	case key.Matches(msg, s.keys.openURL):
		return openURLCmd(i.chapter)
	}
	return s.List.Update(msg)
}

func (s *state) selectAll() {
	for _, i := range s.items() {
		if !i.selected && i.unavailable() == "" {
			i.toggle()
			s.selected.Put(i)
		}
	}
}

// refresh finds where the chapters are downloaded, scan checks the library against the
// files first. It isn't a Load: it doesn't make the State busy for a download.
func (s *state) refresh(ctx context.Context, scan bool) tea.Cmd {
	items := s.items()
	chapters := make([]mangadata.Chapter, len(items))
	for i, it := range items {
		chapters[i] = it.chapter
	}
	c, manga := s.client, s.manga
	root, temp := path.Downloads(), path.Temp()
	providerDirs := config.Download.Provider.CreateDir.Get()
	return func() tea.Msg {
		release, ok := base.Track(ctx)
		if !ok {
			return nil
		}
		defer release()
		msg := filesMsg{items: items, temp: tempFiles(afs.Afero, c, temp, chapters)}
		lib, err := cache.Library()
		if err != nil {
			log.Error("library: %s", err)
			return msg
		}
		var scanner *library.Scanner
		if scan {
			scanner = &library.Scanner{FS: afs.Afero, Library: lib}
		}
		// the chapters still show without it
		m, found, err := lib.MangaFor(c.Info(), manga, providerDirs)
		if err != nil {
			log.Error("library manga of %q: %s", manga, err)
		}
		if found {
			if msg.downloads, err = libraryDownloads(lib, scanner, root, m, providerDirs); err != nil {
				log.Error("library of %q: %s", manga, err)
			}
		}
		history, err := cache.History()
		if err == nil {
			msg.read, err = readChapters(history, c.Info(), manga, m.ID)
		}
		if err != nil {
			log.Error("read history of %q: %s", manga, err)
		}
		return msg
	}
}

func (s *state) setFiles(msg filesMsg) {
	readFormat := config.Read.Format.Get()
	for i, it := range msg.items {
		it.setFiles(msg.downloads[it.chapter.Info().Number], msg.temp[i], readFormat)
		it.read = msg.read[it.chapter.Info().Number]
	}
}

func (s *state) items() []*item {
	items := make([]*item, 0, len(s.List.Items()))
	for _, i := range s.List.Items() {
		if i, ok := i.(*item); ok {
			items = append(items, i)
		}
	}
	return items
}

func (s *state) updateItemHeight() {
	if s.show.date || s.show.group {
		s.List.SetItemHeight(3)
	} else {
		s.List.SetItemHeight(2)
	}
}

func (s *state) updateSubtitleFormats() {
	s.renderedSubtitleFormats = s.renderedSep +
		s.styles.subtitle.Render("download ") +
		s.styles.format.Render(config.Download.Format.Get().String()) +
		s.styles.subtitle.Render(" & read ") +
		s.styles.format.Render(config.Read.Format.Get().String())
}

// updateKeys enables the keys that need a chapter.
func (s *state) updateKeys() {
	enable := len(s.List.Items()) != 0
	for _, k := range []*key.Binding{
		&s.keys.toggle, &s.keys.read, &s.keys.download, &s.keys.openURL, &s.keys.selectAll, &s.keys.unselectAll,
		&s.keys.toggleVolumeNumber, &s.keys.toggleChapterNumber, &s.keys.toggleGroup, &s.keys.toggleDate,
	} {
		k.SetEnabled(enable)
	}
}

func (s *state) View() string {
	switch {
	case s.confirm.Active():
		return s.confirm.View()
	case s.inFormats:
		return util.PlaceCenter(s.size.Width, s.size.Height, s.styles.formatView.Render(s.formats.View()), s.formatsBehind)
	default:
		return s.List.View()
	}
}

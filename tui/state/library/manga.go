package library

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/luevano/mangal/v5/util/afs"
	stringutil "github.com/luevano/mangal/v5/util/string"
	"github.com/skratchdot/open-golang/open"
)

var _ base.State = (*mangaState)(nil)

type downloadItem struct {
	download storage.Download
}

func (i *downloadItem) FilterValue() string { return i.download.ChapterTitle }

func (i *downloadItem) Title() string {
	title := stringutil.FormatFloa32(i.download.Chapter)
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
	library storage.Library
	manga   storage.Manga
	list    *list.Model
	confirm confirmer
	keyMap  mangaKeyMap
}

func newMangaState(lib storage.Library, manga storage.Manga) *mangaState {
	return &mangaState{
		library: lib,
		manga:   manga,
		list:    list.New(2, 1, "chapter", "chapters", []*downloadItem{}, func(i *downloadItem) _list.DefaultItem { return i }),
		confirm: newConfirmer(),
		keyMap:  newMangaKeyMap(),
	}
}

func (s *mangaState) reload() tea.Cmd {
	downloads, err := s.library.Downloads(s.manga.ID)
	if err != nil {
		return func() tea.Msg { return err }
	}
	items := make([]_list.Item, len(downloads))
	for i, dl := range downloads {
		items[i] = &downloadItem{download: dl}
	}
	return s.list.SetItems(items)
}

// mangaScannedMsg is the result of a scan, applied in Update (the list isn't touched from commands).
type mangaScannedMsg struct {
	result library.ScanResult
	manga  storage.Manga
}

// scan reconciles this manga with its folder, the list then shows what's really there.
func (s *mangaState) scan() tea.Msg {
	scanner := library.Scanner{FS: afs.Afero, Library: s.library}
	res, err := scanner.ScanManga(s.manga, path.DownloadsDir(), config.Download.Provider.CreateDir.Get())
	if err != nil {
		return err
	}
	manga := s.manga
	if m, ok, err := s.library.MangaByID(s.manga.ID); err == nil && ok {
		manga = m
	}
	return mangaScannedMsg{result: res, manga: manga}
}

func (s *mangaState) onScanned(msg mangaScannedMsg) tea.Cmd {
	s.manga = msg.manga
	cmds := []tea.Cmd{s.reload()}
	res := msg.result
	if len(res.Missing) != 0 {
		cmds = append(cmds, base.Notify("the manga's folder wasn't found, a library scan looks everywhere"))
	} else if res.Added+res.Moved+res.Removed+res.Relocated != 0 {
		cmds = append(cmds, base.Notify(fmt.Sprintf("updated from the files: %d added, %d moved, %d removed", res.Added, res.Moved, res.Removed)))
	}
	return tea.Batch(cmds...)
}

func (s *mangaState) Backable() bool { return s.list.Unfiltered() && !s.confirm.active }
func (s *mangaState) KeyMap() help.KeyMap {
	return base.CombinedKeyMap(s.keyMap, s.list.KeyMap)
}
func (s *mangaState) Title() base.Title { return base.Title{Text: s.manga.Title} }
func (s *mangaState) Subtitle() string  { return s.manga.Path() }
func (s *mangaState) Status() string    { return s.list.Status() }
func (s *mangaState) Resize(size base.Size) tea.Cmd {
	s.confirm.size = size
	return s.list.Resize(size)
}

func (s *mangaState) Init(ctx context.Context) tea.Cmd {
	return tea.Sequence(s.list.Init(), s.scan)
}

func (s *mangaState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	if cmd, done := s.confirm.update(msg); done {
		return cmd
	}
	if msg, ok := msg.(mangaScannedMsg); ok {
		return s.onScanned(msg)
	}
	if msg, ok := msg.(reloadMsg); ok {
		return tea.Batch(s.reload(), base.Notify(msg.notify))
	}
	if msg, ok := msg.(tea.KeyMsg); ok && !s.list.Filtering() {
		switch {
		case key.Matches(msg, s.keyMap.scan):
			return s.scan
		case key.Matches(msg, s.keyMap.delete):
			item, ok := s.list.SelectedItem().(*downloadItem)
			if !ok {
				break
			}
			dl := item.download
			message := fmt.Sprintf("Delete chapter %s (%s)? The file goes to the trash.", item.Title(), dl.Format)
			s.confirm.askDelete("Delete", message, s.View(), func() tea.Msg {
				if err := library.DeleteDownload(afs.Afero, s.library, path.DownloadsDir(), s.manga, dl); err != nil {
					return err
				}
				return reloadMsg{notify: fmt.Sprintf("deleted chapter %s (in the trash until emptied)", item.Title())}
			})
			return nil
		case key.Matches(msg, s.keyMap.open):
			if item, ok := s.list.SelectedItem().(*downloadItem); ok {
				return func() tea.Msg {
					if err := open.Run(item.download.Path); err != nil {
						return err
					}
					return nil
				}
			}
		}
	}
	return s.list.Update(msg)
}

func (s *mangaState) View() string {
	if s.confirm.active {
		return s.confirm.view()
	}
	return s.list.View()
}

type mangaKeyMap struct {
	open, scan, delete key.Binding
}

func newMangaKeyMap() mangaKeyMap {
	return mangaKeyMap{
		open:   util.Bind("open", "enter"),
		scan:   util.Bind("scan", "s"),
		delete: util.Bind("delete", "d"),
	}
}

func (k mangaKeyMap) ShortHelp() []key.Binding  { return []key.Binding{k.open, k.scan, k.delete} }
func (k mangaKeyMap) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

// Package library is the TUI's library: the downloaded mangas, their chapters and the trash.
package library

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/luevano/mangal/v5/util/afs"
	"github.com/luevano/mangal/v5/util/cache"
)

var _ base.State = (*mangasState)(nil)

type mangaItem struct {
	manga   storage.Manga
	missing bool
	keep    bool
}

func (i *mangaItem) FilterValue() string { return i.manga.Title }

func (i *mangaItem) Title() string {
	title := i.manga.Title
	if i.keep {
		title += " (keep)"
	}
	if i.missing {
		title += " (missing)"
	}
	return title
}

func (i *mangaItem) Description() string {
	parts := []string{fmt.Sprintf("%d chapters", i.manga.Chapters)}
	var sources []string
	for _, src := range i.manga.Sources {
		sources = append(sources, src.Provider)
	}
	if len(sources) != 0 {
		parts = append(parts, strings.Join(sources, ", "))
	}
	return strings.Join(append(parts, i.manga.Path()), " · ")
}

type mangasState struct {
	library storage.Library
	list    *list.Model
	confirm confirmer
	keep    *mangaItem
	trash   string
	keyMap  mangasKeyMap
}

// New is the library: the downloaded mangas.
func New() (base.State, error) {
	_, dataDB, err := cache.DBs()
	if err != nil {
		return nil, err
	}
	s := &mangasState{
		library: storage.Library{DB: dataDB},
		list:    list.New(2, 1, "manga", "mangas", []*mangaItem{}, func(i *mangaItem) _list.DefaultItem { return i }),
		confirm: newConfirmer(),
		keyMap:  newMangasKeyMap(),
	}
	return s, nil
}

func (s *mangasState) reload() tea.Cmd {
	mangas, err := s.library.Mangas()
	if err != nil {
		return func() tea.Msg { return err }
	}
	items := make([]_list.Item, len(mangas))
	for i, m := range mangas {
		exists, _ := afs.Afero.DirExists(m.Path())
		item := &mangaItem{manga: m, missing: !exists}
		if s.keep != nil && s.keep.manga.ID == m.ID {
			item.keep = true
			s.keep = item
		}
		items[i] = item
	}
	s.trash = trashSummary()
	return s.list.SetItems(items)
}

// trashSummary is the size of the trash, empty if it's empty.
func trashSummary() string {
	batches, err := library.Trash(afs.Afero, path.DownloadsDir())
	if err != nil || len(batches) == 0 {
		return ""
	}
	files := 0
	for _, b := range batches {
		files += b.Files
	}
	return fmt.Sprintf("%d files in the trash", files)
}

func (s *mangasState) Backable() bool { return s.list.Unfiltered() && !s.confirm.active }
func (s *mangasState) KeyMap() help.KeyMap {
	return base.CombinedKeyMap(s.keyMap, s.list.KeyMap)
}
func (s *mangasState) Title() base.Title { return base.Title{Text: "Library"} }
func (s *mangasState) Subtitle() string  { return s.list.Subtitle() }
func (s *mangasState) Status() string {
	if s.trash != "" {
		return s.trash + " · " + s.list.Status()
	}
	return s.list.Status()
}

func (s *mangasState) Resize(size base.Size) tea.Cmd {
	s.confirm.size = size
	return s.list.Resize(size)
}

// reloadMsg asks the list to reload from the library, in Update.
type reloadMsg struct {
	notify string
}

func reloaded(notify string) tea.Cmd {
	return func() tea.Msg { return reloadMsg{notify: notify} }
}

func (s *mangasState) Init(ctx context.Context) tea.Cmd {
	return tea.Sequence(s.list.Init(), s.reload())
}

func (s *mangasState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	if cmd, done := s.confirm.update(msg); done {
		return cmd
	}
	switch msg := msg.(type) {
	case base.RestoredMsg:
		return s.reload()
	case scannedMsg:
		return s.onScanned(msg.result)
	case reloadMsg:
		if msg.notify == "" {
			return s.reload()
		}
		return tea.Batch(s.reload(), base.Notify(msg.notify))
	case tea.KeyMsg:
		if s.list.Filtering() {
			break
		}
		switch {
		case key.Matches(msg, s.keyMap.scan):
			return s.scanCmd()
		case key.Matches(msg, s.keyMap.trash):
			return func() tea.Msg { return newTrashState() }
		}
		item, ok := s.list.SelectedItem().(*mangaItem)
		if !ok {
			break
		}
		switch {
		case key.Matches(msg, s.keyMap.confirm):
			return func() tea.Msg { return newMangaState(s.library, item.manga) }
		case key.Matches(msg, s.keyMap.merge):
			return s.mergeCmd(item)
		case key.Matches(msg, s.keyMap.delete):
			message := fmt.Sprintf("Delete %q? Its folder (%d chapters) goes to the trash and it leaves the library.", item.manga.Title, item.manga.Chapters)
			if item.manga.Dir == "" {
				message = fmt.Sprintf("Delete %q? Its %d chapters go to the trash and it leaves the library.", item.manga.Title, item.manga.Chapters)
			}
			s.confirm.askDelete("Delete", message, s.View(), func() tea.Msg {
				if err := library.DeleteManga(afs.Afero, s.library, path.DownloadsDir(), item.manga); err != nil {
					return err
				}
				return reloadMsg{notify: fmt.Sprintf("deleted %s (in the trash until emptied)", item.manga.Title)}
			})
			if s.keep == item {
				s.keep, item.keep = nil, false
			}
			return nil
		case key.Matches(msg, s.keyMap.remove):
			if !item.missing {
				return base.Notify("only mangas whose folder is missing can be removed from the library")
			}
			s.confirm.ask("Remove", fmt.Sprintf("Forget %q? Its folder is gone.", item.manga.Title), s.View(), func() tea.Msg {
				if err := s.library.RemoveManga(item.manga.ID); err != nil {
					return err
				}
				return reloadMsg{notify: "removed " + item.manga.Title}
			})
			return nil
		}
	}
	return s.list.Update(msg)
}

// scannedMsg is a finished library scan.
type scannedMsg struct {
	result library.ScanResult
}

func (s *mangasState) scanCmd() tea.Cmd {
	return tea.Sequence(
		base.Loading("Scanning the library"),
		func() tea.Msg {
			scanner := library.Scanner{FS: afs.Afero, Library: s.library}
			res, err := scanner.Scan(path.DownloadsDir(), config.Download.Provider.CreateDir.Get())
			if err != nil {
				return err
			}
			return scannedMsg{result: res}
		},
		base.Loaded,
	)
}

func (s *mangasState) onScanned(res library.ScanResult) tea.Cmd {
	root := path.DownloadsDir()
	if dir := library.OtherRoot(afs.Afero, res.Missing, root, config.Download.Provider.CreateDir.Get()); dir != "" {
		message := fmt.Sprintf("%d mangas are in %s, but download.path is %s. Use %s as download.path?", len(res.Missing), dir, root, dir)
		s.confirm.ask("Download path", message, s.View(), func() tea.Msg {
			if err := config.WriteKey(config.Download.Path.Key, dir); err != nil {
				return err
			}
			return s.scanCmd()()
		})
		return s.reload()
	}

	report := fmt.Sprintf("Added %d, moved %d, removed %d chapters.\nFound %d moved manga folders, %d new.",
		res.Added, res.Moved, res.Removed, res.Relocated, res.New)
	if len(res.Missing) != 0 {
		report += fmt.Sprintf("\n\n%d mangas weren't found (marked missing in the list, x removes one):", len(res.Missing))
		for _, m := range res.Missing {
			report += "\n  " + m.Title + "  " + m.Path()
		}
	}
	if len(res.Unrecognized) != 0 {
		report += fmt.Sprintf("\n\n%d files without a chapter number:", len(res.Unrecognized))
		for _, f := range res.Unrecognized {
			report += "\n  " + f
		}
	}
	return tea.Batch(s.reload(), base.Viewport("Library scan", report, color.Accent))
}

func (s *mangasState) mergeCmd(item *mangaItem) tea.Cmd {
	switch {
	case s.keep == nil:
		s.keep, item.keep = item, true
		return base.Notify(fmt.Sprintf("keeping %q, select the manga to merge into it and press m", item.manga.Title))
	case s.keep == item:
		s.keep, item.keep = nil, false
		return base.Notify("merge cancelled")
	}
	keep := s.keep
	plan, err := library.PlanMerge(afs.Afero, s.library, keep.manga, item.manga)
	if err != nil {
		return func() tea.Msg { return err }
	}
	message := fmt.Sprintf("Merge %q into %q? %d chapters move, %d already there go to the trash.",
		item.manga.Title, keep.manga.Title, len(plan.Moves), len(plan.Trashed))
	s.confirm.ask("Merge", message, s.View(), func() tea.Msg {
		if err := library.ApplyMerge(afs.Afero, s.library, path.DownloadsDir(), plan); err != nil {
			return err
		}
		s.keep = nil
		return reloadMsg{notify: "merged into " + keep.manga.Title}
	})
	return nil
}

func (s *mangasState) View() string {
	if s.confirm.active {
		return s.confirm.view()
	}
	return s.list.View()
}

type mangasKeyMap struct {
	confirm, scan, merge, remove, trash, delete key.Binding
}

func newMangasKeyMap() mangasKeyMap {
	return mangasKeyMap{
		confirm: util.Bind("chapters", "enter"),
		scan:    util.Bind("scan", "s"),
		merge:   util.Bind("merge", "m"),
		remove:  util.Bind("remove missing", "x"),
		trash:   util.Bind("trash", "t"),
		delete:  util.Bind("delete", "d"),
	}
}

func (k mangasKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.confirm, k.scan, k.merge, k.delete, k.trash}
}

func (k mangasKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{append(k.ShortHelp(), k.remove)}
}

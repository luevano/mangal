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
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/client/kitsu"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/confirm"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/state/watches"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/spf13/afero"
)

var _ base.State = (*mangasState)(nil)

type mangaItem struct {
	manga   storage.Manga
	missing bool
	keep    bool
	watched bool
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
	if i.watched {
		title += " " + watches.Mark()
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
	list.Screen
	files library.Files
	// err is opening the library, shown by Init
	err     error
	confirm *confirm.Dialog
	// keep is the manga a merge keeps, 0 if none
	keep  int64
	trash string
	keys  mangasKeyMap
}

type mangasKeyMap struct {
	confirm, scan, check, merge, remove, trash, delete key.Binding
}

type mangasLoadedMsg struct {
	items []*mangaItem
	trash string
}

// scannedMsg is a finished library scan, otherRoot a directory that has the missing mangas.
// checked is a scan with an id check, its checks or checkErr.
type scannedMsg struct {
	result    library.ScanResult
	otherRoot string
	checked   bool
	checks    []library.IDCheck
	checkErr  error
}

type mergePlannedMsg struct {
	keep, other storage.Manga
	plan        library.MergePlan
}

// setRootMsg is the confirmed new download.path, check the id check of the scan to redo.
type setRootMsg struct {
	dir   string
	check bool
}

// New is the library: the downloaded mangas.
func New() base.State {
	files, err := client.LibraryFiles()
	s := newMangasState(files)
	s.err = err
	return s
}

func newMangasState(files library.Files) *mangasState {
	s := &mangasState{
		files:   files,
		confirm: newDialog(),
		keys: mangasKeyMap{
			confirm: util.Bind("chapters", "enter"),
			scan:    util.Bind("scan", "s"),
			check:   util.Bind("scan and check ids", "S"),
			merge:   util.Bind("merge", "m"),
			remove:  util.Bind("remove missing", "x"),
			trash:   util.Bind("trash", "t"),
			delete:  util.Bind("delete", "d"),
		},
	}
	s.Screen = list.NewScreen(list.New(2, 1, "manga", "mangas", []*mangaItem{}, func(i *mangaItem) _list.DefaultItem { return i }), s.keyMap)
	return s
}

func (s *mangasState) keyMap() help.KeyMap {
	k := s.keys
	short := []key.Binding{k.confirm, k.scan, k.merge, k.delete, k.trash}
	return util.KeyMap{Short: short, Full: [][]key.Binding{append(short, k.check, k.remove)}}
}

func (s *mangasState) reload(ctx context.Context) tea.Cmd {
	files := s.files
	return base.Load(ctx, "", func(ctx context.Context) (mangasLoadedMsg, error) {
		mangas, err := files.Library.Mangas()
		if err != nil {
			return mangasLoadedMsg{}, err
		}
		watched, _ := watches.Watched(ctx)
		items := make([]*mangaItem, len(mangas))
		for i, m := range mangas {
			exists, _ := afero.DirExists(files.FS, m.Path())
			items[i] = &mangaItem{manga: m, missing: !exists, watched: watched.HasLibrary(m)}
		}
		return mangasLoadedMsg{items: items, trash: trashSummary(files)}, nil
	}, func(msg mangasLoadedMsg) tea.Msg { return msg })
}

// trashSummary is the size of the trash, empty if it's empty.
func trashSummary(files library.Files) string {
	batches, err := library.Trash(files.FS, files.Root)
	if err != nil || len(batches) == 0 {
		return ""
	}
	n := 0
	for _, b := range batches {
		n += b.Files
	}
	return fmt.Sprintf("%d files in the trash", n)
}

func (s *mangasState) Backable() bool  { return s.Screen.Backable() && !s.confirm.Active() }
func (s *mangasState) Inputting() bool { return s.Screen.Inputting() || s.confirm.Active() }
func (s *mangasState) Title() base.Title {
	return base.Title{Text: "Library"}
}

func (s *mangasState) Status() string {
	status := s.List.Status()
	switch {
	case s.trash == "":
		return status
	case status == "":
		return s.trash
	}
	return s.trash + " · " + status
}

func (s *mangasState) Resize(size base.Size) tea.Cmd {
	s.confirm.Resize(size)
	return s.Screen.Resize(size)
}

func (s *mangasState) Init(ctx context.Context) tea.Cmd {
	if s.err != nil {
		err := s.err
		return func() tea.Msg { return err }
	}
	return s.reload(ctx)
}

func (s *mangasState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	if msg, ok := msg.(tea.KeyMsg); ok && s.confirm.Active() {
		return s.confirm.Update(msg)
	}
	switch msg := msg.(type) {
	case mangasLoadedMsg:
		return s.onLoaded(msg)
	case base.RestoredMsg:
		return s.reload(ctx)
	case scannedMsg:
		return tea.Batch(s.reload(ctx), s.onScanned(msg))
	case mergePlannedMsg:
		s.askMerge(msg)
		return nil
	case setRootMsg:
		// in Update, the config isn't safe to write from the background
		if err := config.WriteKey(config.Download.Path.Key, msg.dir); err != nil {
			return func() tea.Msg { return err }
		}
		s.files.Root = path.Downloads()
		return s.scanCmd(ctx, msg.check)
	case actionMsg:
		return msg.run(ctx)
	case reloadMsg:
		if msg.merged {
			s.keep = 0
		}
		return withNotify(s.reload(ctx), msg.notify)
	case tea.KeyMsg:
		if s.List.Filtering() || s.err != nil {
			break
		}
		if cmd, ok := s.updateKey(ctx, msg); ok {
			return cmd
		}
	}
	return s.List.Update(msg)
}

func (s *mangasState) updateKey(ctx context.Context, msg tea.KeyMsg) (tea.Cmd, bool) {
	switch {
	case key.Matches(msg, s.keys.scan), key.Matches(msg, s.keys.check):
		if base.Busy(ctx) {
			return nil, true
		}
		return s.scanCmd(ctx, key.Matches(msg, s.keys.check)), true
	case key.Matches(msg, s.keys.trash):
		files := s.files
		return func() tea.Msg { return newTrashState(files) }, true
	}
	item, ok := s.List.SelectedItem().(*mangaItem)
	if !ok {
		return nil, false
	}
	files := s.files
	switch {
	case key.Matches(msg, s.keys.confirm):
		return func() tea.Msg { return newMangaState(files, item.manga) }, true
	case key.Matches(msg, s.keys.merge):
		return s.merge(ctx, item), true
	case key.Matches(msg, s.keys.delete):
		m := item.manga
		message := fmt.Sprintf("Delete %q? Its folder (%d chapters) goes to the trash and it leaves the library.", m.Title, m.Chapters)
		if m.Dir == "" {
			message = fmt.Sprintf("Delete %q? Its %d chapters go to the trash and it leaves the library.", m.Title, m.Chapters)
		}
		s.confirm.AskDelete("Delete", message, s.View(), action("Deleting "+m.Title, func() (string, error) {
			return fmt.Sprintf("deleted %s (in the trash until emptied)", m.Title), files.DeleteManga(m)
		}))
		return nil, true
	case key.Matches(msg, s.keys.remove):
		if !item.missing {
			return base.Notify("only mangas whose folder is missing can be removed from the library"), true
		}
		m := item.manga
		s.confirm.Ask("Remove", fmt.Sprintf("Forget %q? Its folder is gone.", m.Title), s.View(), action("Removing "+m.Title, func() (string, error) {
			return "removed " + m.Title, files.Library.RemoveManga(m.ID)
		}))
		return nil, true
	}
	return nil, false
}

func (s *mangasState) onLoaded(msg mangasLoadedMsg) tea.Cmd {
	s.trash = msg.trash
	kept := false
	items := make([]_list.Item, len(msg.items))
	for i, item := range msg.items {
		if s.keep != 0 && item.manga.ID == s.keep {
			item.keep, kept = true, true
		}
		items[i] = item
	}
	// e.g. it was deleted
	if !kept {
		s.keep = 0
	}
	return s.List.SetItems(items)
}

func (s *mangasState) scanCmd(ctx context.Context, check bool) tea.Cmd {
	files := s.files
	providerDirs := config.Download.Provider.CreateDir.Get()
	return base.Load(ctx, "Scanning the library", func(ctx context.Context) (scannedMsg, error) {
		root, err := path.Ensure(files.Root)
		if err != nil {
			return scannedMsg{}, err
		}
		scanner := library.Scanner{FS: files.FS, Library: files.Library}
		res, err := scanner.Scan(root, providerDirs)
		if err != nil {
			return scannedMsg{}, err
		}
		msg := scannedMsg{result: res, otherRoot: library.OtherRoot(files.FS, res.Missing, root, providerDirs), checked: check}
		if check && msg.otherRoot == "" {
			msg.checks, msg.checkErr = checkIDs(ctx, files)
		}
		return msg, nil
	}, func(msg scannedMsg) tea.Msg { return msg })
}

// checkIDs checks the ids of the adopted folders on AniList and Kitsu.
func checkIDs(ctx context.Context, files library.Files) ([]library.IDCheck, error) {
	mangas, err := files.Library.Mangas()
	if err != nil {
		return nil, err
	}
	ani, err := anilist.Anilist()
	if err != nil {
		return nil, err
	}
	_ = anilist.EnsureLogin(ctx)
	kt, err := kitsu.Kitsu()
	if err != nil {
		return nil, err
	}
	checker := library.IDChecker{
		FS:        files.FS,
		Providers: map[metadata.IDSource]library.IDProvider{metadata.IDSourceAnilist: ani, metadata.IDSourceKitsu: kt},
		Suggest:   ani,
	}
	return checker.Check(ctx, mangas)
}

func (s *mangasState) onScanned(msg scannedMsg) tea.Cmd {
	if dir := msg.otherRoot; dir != "" {
		message := fmt.Sprintf("%d mangas are in %s, but download.path is %s. Use %s as download.path?", len(msg.result.Missing), dir, s.files.Root, dir)
		s.confirm.Ask("Download path", message, s.View(), func() tea.Msg { return setRootMsg{dir: dir, check: msg.checked} })
		return nil
	}
	report := scanReport(msg.result)
	if msg.checked {
		report += "\n\n" + checkReport(msg.checks, msg.checkErr)
	}
	return base.Viewport("Library scan", report, color.Accent)
}

func checkReport(checks []library.IDCheck, err error) string {
	switch {
	case err != nil:
		return "Checking the ids: " + err.Error()
	case len(checks) == 0:
		return "The ids of the folders without a .libmangal.json look right."
	}
	report := fmt.Sprintf("Check the ids of %d folders without a .libmangal.json:", len(checks))
	for _, c := range checks {
		report += "\n  " + c.String()
	}
	return report
}

func scanReport(res library.ScanResult) string {
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
	return report
}

// merge marks the manga to keep, the next one is merged into it.
func (s *mangasState) merge(ctx context.Context, item *mangaItem) tea.Cmd {
	switch {
	case s.keep == 0:
		s.keep, item.keep = item.manga.ID, true
		return base.Notify(fmt.Sprintf("keeping %q, select the manga to merge into it and press m", item.manga.Title))
	case s.keep == item.manga.ID:
		s.keep, item.keep = 0, false
		return base.Notify("merge cancelled")
	}
	var keep storage.Manga
	for _, i := range s.List.Items() {
		if i := i.(*mangaItem); i.manga.ID == s.keep {
			keep = i.manga
		}
	}
	if base.Busy(ctx) {
		return nil
	}
	files, other := s.files, item.manga
	return base.Load(ctx, "Planning the merge", func(context.Context) (mergePlannedMsg, error) {
		plan, err := files.PlanMerge(keep, other)
		return mergePlannedMsg{keep: keep, other: other, plan: plan}, err
	}, func(msg mergePlannedMsg) tea.Msg { return msg })
}

func (s *mangasState) askMerge(msg mergePlannedMsg) {
	message := fmt.Sprintf("Merge %q into %q? %d chapters move, %d already there go to the trash.",
		msg.other.Title, msg.keep.Title, len(msg.plan.Moves), len(msg.plan.Trashed))
	files := s.files
	s.confirm.Ask("Merge", message, s.View(), func() tea.Msg {
		return actionMsg{label: "Merging", do: func() (reloadMsg, error) {
			return reloadMsg{notify: "merged into " + msg.keep.Title, merged: true}, files.ApplyMerge(msg.plan)
		}}
	})
}

func (s *mangasState) View() string {
	if s.confirm.Active() {
		return s.confirm.View()
	}
	return s.List.View()
}

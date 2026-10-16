// Package library is the TUI's library: the downloaded mangas, their chapters and the trash.
package library

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	_list "charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/state/watches"
	"github.com/luevano/mangal/v5/tui/util"
)

var _ base.State = (*mangasState)(nil)

type mangaItem struct {
	manga   storage.Manga
	missing bool
	keep    bool
	watches []watch.Watch
	// duplicate shares a metadata id with another manga, dupKeep is the suggested keep
	// (0 when it's the keep)
	duplicate bool
	dupKeep   int64
}

func (i *mangaItem) FilterValue() string { return i.manga.Title }
func (i *mangaItem) ItemKey() string     { return strconv.FormatInt(i.manga.ID, 10) }

func (i *mangaItem) Title() string {
	title := i.manga.Title
	if i.keep {
		title += " (keep)"
	}
	if i.missing {
		title += " (missing)"
	}
	if i.duplicate {
		title += " (duplicate)"
	}
	if len(i.watches) != 0 {
		title += " " + watches.Mark()
	}
	return title
}

func (i *mangaItem) Description() string {
	parts := []string{fmt.Sprintf("%d chapters", i.manga.Chapters)}
	if status := library.StatusLabel(i.manga.Status); status != "" {
		parts = append(parts, status)
	}
	if sources := library.SourcesSummary(i.manga.Sources); sources != "" {
		parts = append(parts, sources)
	}
	return strings.Join(append(parts, i.manga.Path()), " · ")
}

type mangasState struct {
	list.Screen
	// keep is the manga a merge keeps, 0 if none
	keep  int64
	trash string
	// items are all the mangas, the list shows them or only the duplicates
	items      []*mangaItem
	dups       []library.Duplicate
	duplicates bool
	// pendingMerge is merged into keep once the mangas load (NewMerge), 0 if none
	pendingMerge int64
	keys         mangasKeyMap
}

type mangasKeyMap struct {
	confirm, scan, check, merge, move, trash, delete, watch, undo, duplicates, links, backups, rename key.Binding
}

type mangasLoadedMsg struct {
	items []*mangaItem
	dups  []library.Duplicate
	trash string
}

// scannedMsg is a finished library scan, otherRoot a directory that has the missing mangas.
// checked is a scan with an id check, its checks or checkErr.
type scannedMsg struct {
	root      string
	result    library.ScanResult
	otherRoot string
	checked   bool
	checks    []library.IDCheck
	checkErr  error
	dups      []library.Duplicate
}

type mergePlannedMsg struct {
	keep, other storage.Manga
	plan        core.Plan
}

// setRootMsg is the confirmed new download.path, check the id check of the scan to redo.
type setRootMsg struct {
	dir   string
	check bool
}

// New is the library: the downloaded mangas.
func New() base.State {
	return newMangasState()
}

func newMangasState() *mangasState {
	s := &mangasState{
		keys: mangasKeyMap{
			confirm: util.Bind("chapters", "enter"),
			scan:    base.KeyScan,
			check:   base.KeyScanIDs,
			merge:   base.KeyMerge,
			move:    base.KeyMove,
			trash:   base.KeyTrash,
			delete:  base.KeyDelete,
			undo:    base.KeyUndo,
			watch:   base.KeyItsWatch,
			// screen-wide filter like the watches' needs re-link and not in library
			duplicates: base.KeyDuplicates,
			links:      base.KeyBindings,
			backups:    base.KeyBackups,
			rename:     base.KeyRename,
		},
	}
	s.Screen = list.NewScreen(list.New(2, 1, "manga", "mangas", []*mangaItem{}, func(i *mangaItem) _list.DefaultItem { return i }), s.keyMap)
	s.Confirm = newDialog()
	return s
}

func (s *mangasState) keyMap() help.KeyMap {
	k := s.keys
	short := []key.Binding{k.confirm, k.scan, k.merge, k.delete, k.trash, k.undo}
	return util.KeyMap{Short: short, Full: [][]key.Binding{append(short, k.move, k.rename, k.check, k.watch, k.duplicates, k.links, k.backups)}}
}

func (s *mangasState) reload(ctx context.Context) tea.Cmd {
	return base.Load(ctx, "", func(ctx context.Context) (mangasLoadedMsg, error) {
		lib, err := base.Library()
		if err != nil {
			return mangasLoadedMsg{}, err
		}
		// the statuses from the cached metadata
		list, err := lib.Mangas(ctx, core.MangasOptions{Statuses: true, Missing: true})
		if err != nil {
			return mangasLoadedMsg{}, err
		}
		if list.StatusError != "" {
			// a mark isn't worth an error screen
			log.Warn("publication statuses: %s", list.StatusError)
		}
		watched, err := watches.Watched(ctx)
		if err != nil {
			// the list without the watch marks
			log.Warn("library: the watches: %s", err)
		}
		dups, err := lib.Duplicates(ctx)
		if err != nil {
			log.Warn("duplicates: %s", err)
		}
		ids := library.DuplicateIDs(dups)
		items := make([]*mangaItem, len(list.Mangas))
		for i, m := range list.Mangas {
			keep, dup := ids[m.ID]
			items[i] = &mangaItem{manga: m, missing: list.IsMissing(m.ID), watches: watched.ForLibrary(m), duplicate: dup, dupKeep: keep}
		}
		return mangasLoadedMsg{items: items, dups: dups, trash: trashSummary(ctx, lib)}, nil
	}, func(msg mangasLoadedMsg) tea.Msg { return msg })
}

// trashSummary is the size of the trash, empty if it's empty.
func trashSummary(ctx context.Context, lib core.Library) string {
	batches, err := lib.Trash(ctx)
	if err != nil || len(batches) == 0 {
		return ""
	}
	n := 0
	for _, b := range batches {
		n += b.Files
	}
	return fmt.Sprintf("%d files in the trash", n)
}

func (s *mangasState) Key() string { return "library" }

func (s *mangasState) Title() base.Title {
	return base.Title{Text: "Library"}
}

func (s *mangasState) Subtitle() string {
	parts := []string{s.List.Subtitle()}
	n := 0
	for _, i := range s.items {
		if i.duplicate {
			n++
		}
	}
	switch {
	case s.duplicates:
		parts = append(parts, "duplicates")
	case n > 0:
		parts = append(parts, fmt.Sprintf("%d duplicates (%s)", n, s.keys.duplicates.Help().Key))
	}
	return strings.Join(parts, " · ")
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

func (s *mangasState) Init(ctx context.Context) tea.Cmd {
	return s.reload(ctx)
}

func (s *mangasState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	if cmd, ok := s.Answer(msg); ok {
		return cmd
	}
	switch msg := msg.(type) {
	case mangasLoadedMsg:
		cmd := s.onLoaded(msg)
		if other := s.pendingMerge; other != 0 {
			s.pendingMerge = 0
			for _, item := range s.items {
				if item.manga.ID == other {
					s.List.SelectFunc(func(i _list.Item) bool { return i == _list.Item(item) })
					return tea.Batch(cmd, s.merge(ctx, item))
				}
			}
			return tea.Batch(cmd, base.Notify("Nothing to merge any more"))
		}
		return cmd
	case base.RestoredMsg:
		// a merge asked for (health): its keep in the duplicates
		if r, ok := msg.Request.(*mangasState); ok && r.pendingMerge != 0 {
			s.keep, s.duplicates, s.pendingMerge = r.keep, true, r.pendingMerge
		}
		return s.reload(ctx)
	case scannedMsg:
		return tea.Batch(s.reload(ctx), s.onScanned(msg))
	case mergePlannedMsg:
		s.askMerge(msg)
		return nil
	case undoPlannedMsg:
		return s.askUndo(msg)
	case backupsFoundMsg:
		return askBackups(s.Confirm, s.View(), msg)
	case setRootMsg:
		// in Update, the config isn't safe to write from the background; there may be no
		// file yet
		c, err := base.Core()
		if err == nil {
			_, err = c.Config.Set(ctx, config.Download.Path.Key, msg.dir)
		}
		if err != nil {
			return func() tea.Msg { return err }
		}
		return s.scanCmd(ctx, msg.check)
	case actionMsg:
		return msg.run(ctx)
	case reloadMsg:
		if msg.merged {
			s.keep = 0
		}
		return withNotify(s.reload(ctx), msg.notify)
	case tea.KeyPressMsg:
		if s.List.Filtering() {
			break
		}
		if cmd, ok := s.updateKey(ctx, msg); ok {
			return cmd
		}
	}
	return s.List.Update(msg)
}

func (s *mangasState) updateKey(ctx context.Context, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch {
	case key.Matches(msg, s.keys.scan), key.Matches(msg, s.keys.check):
		if base.Busy(ctx) {
			return nil, true
		}
		return s.scanCmd(ctx, key.Matches(msg, s.keys.check)), true
	case key.Matches(msg, s.keys.trash):
		return func() tea.Msg { return newTrashState() }, true
	case key.Matches(msg, s.keys.undo):
		return s.undo(ctx), true
	case key.Matches(msg, s.keys.rename):
		return func() tea.Msg { return newRenameState(nil) }, true
	case key.Matches(msg, s.keys.backups):
		mangas := make([]storage.Manga, len(s.items))
		for i, item := range s.items {
			mangas[i] = item.manga
		}
		return findBackups(ctx, mangas), true
	case key.Matches(msg, s.keys.duplicates):
		s.duplicates = !s.duplicates
		cmd := s.setItems()
		if s.duplicates && len(s.List.Items()) == 0 {
			cmd = tea.Batch(cmd, base.Notify("No two mangas share a metadata id"))
		}
		return cmd, true
	}
	item, ok := s.List.SelectedItem().(*mangaItem)
	if !ok {
		return nil, false
	}
	if key.Matches(msg, s.keys.delete) && base.Busy(ctx) {
		return base.Notify(busyNotice), true
	}
	switch {
	case key.Matches(msg, s.keys.confirm):
		return func() tea.Msg { return newMangaState(item.manga) }, true
	case key.Matches(msg, s.keys.merge):
		return s.merge(ctx, item), true
	case key.Matches(msg, s.keys.move):
		return moveCmd(item.manga), true
	case key.Matches(msg, s.keys.links):
		return linksCmd(item.manga), true
	case key.Matches(msg, s.keys.watch):
		return watches.JumpCmd(item.watches, ""), true
	case key.Matches(msg, s.keys.delete) && !item.missing:
		m := item.manga
		message := fmt.Sprintf("Delete %q? Its folder (%d chapters) goes to the trash and it leaves the library.", m.Title, m.Chapters)
		if m.Dir == "" {
			message = fmt.Sprintf("Delete %q? Its %d chapters go to the trash and it leaves the library.", m.Title, m.Chapters)
		}
		s.Confirm.AskDelete("Delete", message, s.View(), change("Deleting "+m.Title, core.DeleteReq{Manga: m.ID}, fmt.Sprintf("deleted %s (in the trash until emptied)", m.Title)))
		return nil, true
	case key.Matches(msg, s.keys.delete):
		// its folder is gone: delete forgets it
		m := item.manga
		s.Confirm.AskDelete("Remove", fmt.Sprintf("Forget %q? Its folder is gone.", m.Title), s.View(), change("Removing "+m.Title, core.ForgetReq{Mangas: []int64{m.ID}}, "removed "+m.Title))
		return nil, true
	}
	return nil, false
}

func (s *mangasState) onLoaded(msg mangasLoadedMsg) tea.Cmd {
	s.trash = msg.trash
	s.items, s.dups = msg.items, msg.dups
	kept := false
	for _, item := range msg.items {
		if s.keep != 0 && item.manga.ID == s.keep {
			item.keep, kept = true, true
		}
	}
	// e.g. it was deleted
	if !kept {
		s.keep = 0
	}
	return s.setItems()
}

// setItems shows the mangas, or only the duplicates grouped (the suggested keep first),
// the selected one stays selected.
func (s *mangasState) setItems() tea.Cmd {
	shown := s.items
	if s.duplicates {
		byID := map[int64]*mangaItem{}
		for _, i := range s.items {
			byID[i.manga.ID] = i
		}
		shown = nil
		seen := map[int64]bool{}
		for _, d := range s.dups {
			for _, m := range d.Mangas {
				if i, ok := byID[m.Manga.ID]; ok && !seen[m.Manga.ID] {
					seen[m.Manga.ID] = true
					shown = append(shown, i)
				}
			}
		}
	}
	items := make([]_list.Item, len(shown))
	for n, i := range shown {
		items[n] = i
	}
	return s.List.SetItemsKeep(items)
}

func (s *mangasState) scanCmd(ctx context.Context, check bool) tea.Cmd {
	return base.LoadWithStatus(ctx, "Scanning the library", func(ctx context.Context, status func(string)) (scannedMsg, error) {
		return scan(ctx, check, status)
	}, func(msg scannedMsg) tea.Msg { return msg })
}

// scan scans the library (core's Library.Scan), check checks the ids and the statuses too.
func scan(ctx context.Context, check bool, status func(string)) (scannedMsg, error) {
	c, err := base.Core()
	if err != nil {
		return scannedMsg{}, err
	}
	rep, err := c.Library.Scan(base.WithStatus(ctx, status), core.ScanOptions{Check: check, Statuses: true})
	if err != nil {
		return scannedMsg{}, err
	}
	return scannedMsg{root: rep.Root, result: rep.Result, otherRoot: rep.OtherRoot, checked: check, checks: rep.Checks, checkErr: rep.CheckErr, dups: rep.Duplicates}, nil
}

func (s *mangasState) onScanned(msg scannedMsg) tea.Cmd {
	if dir := msg.otherRoot; dir != "" {
		message := fmt.Sprintf("%d mangas are in %s, but download.path is %s. Use %s as download.path?", len(msg.result.Missing), dir, msg.root, dir)
		s.Confirm.Ask("Download path", message, s.View(), func() tea.Msg { return setRootMsg{dir: dir, check: msg.checked} })
		return nil
	}
	report := scanReport(msg.result)
	if lines := library.DuplicateLines(msg.dups, msg.root); len(lines) != 0 {
		report += "\n\n" + strings.Join(lines, "\n") + "\n(" + s.keys.duplicates.Help().Key + " shows them, " + s.keys.merge.Help().Key + " on one merges it into its keep)"
	}
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
	if n := res.Sources.FromWatches + res.Sources.FromDownloads; n > 0 {
		report += fmt.Sprintf("\n%d sources found from watches/downloads.", n)
	}
	if len(res.Missing) != 0 {
		report += fmt.Sprintf("\n\n%d mangas weren't found (marked missing in the list, %s removes one):", len(res.Missing), base.KeyDelete.Help().Key)
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
	keepID := s.keep
	if keepID == 0 && s.duplicates && item.dupKeep != 0 {
		// a duplicate merges into its suggested keep right away
		keepID = item.dupKeep
	}
	switch {
	case keepID == 0:
		s.keep, item.keep = item.manga.ID, true
		return base.Notify(fmt.Sprintf("keeping %q, select the manga to merge into it and press %s", item.manga.Title, s.keys.merge.Help().Key))
	case keepID == item.manga.ID:
		s.keep, item.keep = 0, false
		return base.Notify("merge cancelled")
	case base.Busy(ctx):
		// nothing marked: the merge isn't on
		return base.Notify(busyNotice)
	}
	s.keep = keepID
	var keep storage.Manga
	for _, i := range s.items {
		if i.manga.ID == s.keep {
			keep, i.keep = i.manga, true
		}
	}
	other := item.manga
	return base.PlanChange(ctx, "Planning the merge", core.MergeReq{Keep: keep.ID, Other: other.ID}, func(p core.Plan) tea.Msg {
		return mergePlannedMsg{keep: keep, other: other, plan: p}
	})
}

func (s *mangasState) askMerge(msg mergePlannedMsg) {
	mp := msg.plan.Detail.(library.MergePlan)
	message := fmt.Sprintf("Merge %q into %q? %d chapters move, %d already there go to the trash.",
		msg.other.Title, msg.keep.Title, len(mp.Moves), len(mp.Trashed))
	plan := msg.plan
	s.Confirm.Ask("Merge", message, s.View(), func() tea.Msg {
		return actionMsg{label: "Merging", do: func(ctx context.Context, status func(string)) (reloadMsg, error) {
			if _, err := base.ApplyPlan(ctx, plan, status); err != nil {
				return reloadMsg{merged: true}, err
			}
			return reloadMsg{notify: undoNotice("merged into " + msg.keep.Title), merged: true}, nil
		}}
	})
}

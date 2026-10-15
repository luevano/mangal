package library

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/luevano/mangal/v5/util/cache"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

var _ base.State = (*renameState)(nil)

type renameItem struct {
	s *renameState
	r library.Rename
	// selected renames it; the skipped ones never are
	selected bool
}

func (i *renameItem) ItemKey() string { return i.r.From }

func (i *renameItem) FilterValue() string {
	return i.s.plan.Rel(i.r.From) + " " + filepath.Base(i.r.To)
}

func (i *renameItem) Title() string {
	title := i.s.plan.Rel(i.r.From)
	if i.selected {
		title += " " + icon.Mark.Colored()
	}
	return title
}

func (i *renameItem) Description() string {
	r := i.r
	switch {
	case r.NoMetadata && r.Skip != "":
		return fmt.Sprintf("skip: %s (would be %s, %s renames it)", r.Skip, filepath.Base(r.To), i.s.keys.fallback.Help().Key)
	case r.Skip != "":
		return "skip: " + r.Skip
	case r.RemoveEmpty:
		return "removed once empty"
	case r.Chapter:
		return "-> " + filepath.Base(r.To)
	}
	return "-> " + i.s.plan.Rel(r.To)
}

// renameState is library rename's plan (the CLI's, the same options): the renames start
// selected, enter renames the selected ones (asks) as one operation that U in the
// library undoes.
type renameState struct {
	list.Screen
	lib storage.Library
	// paths keep only the renames of these folders and provider directories (a health
	// finding's), nil keeps every one
	paths  []string
	opts   core.RenameReq
	plan   client.RenamePlan
	loaded bool
	// unselected are the renames (by From) the user unselected: they stay so on a new plan
	unselected map[string]bool
	keys       renameKeyMap
}

type renameKeyMap struct {
	rename, toggle, all, none, chapters, fallback, refresh, info key.Binding
}

type renamePlannedMsg struct{ plan client.RenamePlan }

// NewRename is library rename's screen, of every folder (paths nil) or only the renames
// of paths (manga folders, provider directories).
func NewRename(paths []string) (base.State, error) {
	lib, err := cache.Library()
	if err != nil {
		return nil, err
	}
	return newRenameState(lib, paths), nil
}

func newRenameState(lib storage.Library, paths []string) *renameState {
	s := &renameState{
		lib:        lib,
		paths:      paths,
		unselected: map[string]bool{},
		keys: renameKeyMap{
			rename:   util.Bind("rename", "enter"),
			toggle:   base.KeyToggle,
			all:      base.KeySelectAll,
			none:     base.KeyUnselectAll,
			chapters: base.KeyChapterFiles,
			fallback: base.KeyFallback,
			refresh:  base.KeyRefresh,
			info:     base.KeyInfo,
		},
	}
	s.Screen = list.NewScreen(list.New(2, 1, "rename", "renames", []*renameItem{}, func(i *renameItem) _list.DefaultItem { return i }), s.keyMap)
	s.Confirm = newDialog()
	return s
}

func (s *renameState) keyMap() help.KeyMap {
	k := s.keys
	short := []key.Binding{k.rename, k.toggle, k.chapters, k.fallback, k.info}
	return util.KeyMap{Short: short, Full: [][]key.Binding{append(short, k.all, k.none, k.refresh)}}
}

func (s *renameState) Key() string {
	if len(s.paths) == 0 {
		return "library-rename"
	}
	return "library-rename/" + strings.Join(s.paths, "|")
}

func (s *renameState) Title() base.Title { return base.Title{Text: "Rename"} }

func (s *renameState) Subtitle() string {
	onOff := func(on bool) string {
		if on {
			return "on"
		}
		return "off"
	}
	parts := []string{s.List.Subtitle()}
	if len(s.paths) != 0 {
		parts = append(parts, "only "+stringutil.Quantify(len(s.paths), "folder", "folders")+" of the finding")
	}
	parts = append(parts,
		fmt.Sprintf("chapter files %s (%s)", onOff(s.opts.Chapters), s.keys.chapters.Help().Key),
		fmt.Sprintf("fallback names %s (%s)", onOff(s.opts.Fallback), s.keys.fallback.Help().Key))
	return strings.Join(parts, " · ")
}

func (s *renameState) Status() string {
	if !s.loaded {
		return s.List.Status()
	}
	selected, skipped := 0, 0
	for _, i := range s.items() {
		switch {
		case i.r.Skip != "":
			skipped++
		case i.selected:
			selected++
		}
	}
	status := fmt.Sprintf("%d selected, %d skipped", selected, skipped)
	if n := len(s.notes()); n != 0 {
		status += fmt.Sprintf(" · %s (%s)", stringutil.Quantify(n, "note", "notes"), s.keys.info.Help().Key)
	}
	return status
}

func (s *renameState) Init(ctx context.Context) tea.Cmd {
	return s.replan(ctx, false)
}

// replan plans again, with fresh AniList metadata with refresh.
func (s *renameState) replan(ctx context.Context, refresh bool) tea.Cmd {
	req := s.opts
	req.Refresh, req.Paths = refresh, s.paths
	label := "Planning the renames"
	if refresh {
		label = "Planning the renames (fresh metadata)"
	}
	return base.LoadWithStatus(ctx, label, func(ctx context.Context, status func(string)) (renamePlannedMsg, error) {
		changes, err := base.Changes()
		if err != nil {
			return renamePlannedMsg{}, err
		}
		p, err := changes.Plan(base.WithStatus(ctx, status), req)
		if err != nil {
			return renamePlannedMsg{}, err
		}
		return renamePlannedMsg{plan: p.Detail.(client.RenamePlan)}, nil
	}, func(msg renamePlannedMsg) tea.Msg { return msg })
}

func (s *renameState) items() []*renameItem {
	items := make([]*renameItem, 0, len(s.List.Items()))
	for _, i := range s.List.Items() {
		if i, ok := i.(*renameItem); ok {
			items = append(items, i)
		}
	}
	return items
}

// selected are the renames to do, in the plan's order (chapter files first).
func (s *renameState) selected() []library.Rename {
	var renames []library.Rename
	for _, i := range s.items() {
		if i.selected {
			renames = append(renames, i.r)
		}
	}
	return renames
}

func (s *renameState) onPlanned(msg renamePlannedMsg) tea.Cmd {
	s.plan, s.loaded = msg.plan, true
	items := make([]_list.Item, len(msg.plan.Renames))
	for i, r := range msg.plan.Renames {
		items[i] = &renameItem{s: s, r: r, selected: r.Skip == "" && !s.unselected[r.From]}
	}
	return s.List.SetItemsKeep(items)
}

func (s *renameState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	if cmd, ok := s.Answer(msg); ok {
		return cmd
	}
	switch msg := msg.(type) {
	case renamePlannedMsg:
		return s.onPlanned(msg)
	case base.RestoredMsg:
		return s.replan(ctx, false)
	case actionMsg:
		return msg.run(ctx)
	case reloadMsg:
		return withNotify(s.replan(ctx, false), msg.notify)
	case tea.KeyMsg:
		if s.List.Filtering() {
			break
		}
		if cmd, ok := s.updateKey(ctx, msg); ok {
			return cmd
		}
	}
	return s.List.Update(msg)
}

func (s *renameState) updateKey(ctx context.Context, msg tea.KeyMsg) (tea.Cmd, bool) {
	k := s.keys
	switch {
	case key.Matches(msg, k.chapters), key.Matches(msg, k.fallback), key.Matches(msg, k.refresh):
		if base.Busy(ctx) {
			return base.Notify(busyNotice), true
		}
		switch {
		case key.Matches(msg, k.chapters):
			s.opts.Chapters = !s.opts.Chapters
		case key.Matches(msg, k.fallback):
			s.opts.Fallback = !s.opts.Fallback
		}
		return s.replan(ctx, key.Matches(msg, k.refresh)), true
	case key.Matches(msg, k.all), key.Matches(msg, k.none):
		all := key.Matches(msg, k.all)
		for _, i := range s.items() {
			if i.r.Skip == "" {
				i.selected = all
				s.unselected[i.r.From] = !all
			}
		}
		return nil, true
	case key.Matches(msg, k.info):
		if !s.loaded {
			return nil, true
		}
		return base.Viewport("Rename", s.info(), color.Accent), true
	case key.Matches(msg, k.rename):
		return s.ask(ctx), true
	}
	i, ok := s.List.SelectedItem().(*renameItem)
	if !ok || !key.Matches(msg, k.toggle) {
		return nil, false
	}
	if i.r.Skip != "" {
		return base.Notify("Can't rename it: " + i.r.Skip), true
	}
	i.selected = !i.selected
	s.unselected[i.r.From] = !i.selected
	return nil, true
}

// notes are what the plan says besides the renames, as library rename prints them.
func (s *renameState) notes() []string {
	p := s.plan
	var notes []string
	if len(p.Disagree) != 0 {
		notes = append(notes, "ids that disagree with the links (the links are used, the links screen shows and fixes them):")
		for _, line := range p.Disagree {
			notes = append(notes, "  "+line)
		}
	}
	if p.Unmarked != 0 {
		notes = append(notes, fmt.Sprintf("%d folders without a %s aren't renamed (the library's %s marks them)", p.Unmarked, libmangal.FilenameMangaDirInfo, base.KeyScanIDs.Help().Key))
	}
	if p.Untitled != 0 {
		notes = append(notes, fmt.Sprintf("%d chapter files kept: no chapter title recorded", p.Untitled))
	}
	if n := len(p.NoMetadata()); n != 0 {
		notes = append(notes, fmt.Sprintf("%d folders not renamed, no metadata found for their ids (the fallback name would drop the year and ids; %s renames them anyway)", n, s.keys.fallback.Help().Key))
	}
	return notes
}

// info is the notes, then the plan as library rename lists it.
func (s *renameState) info() string {
	lines := s.notes()
	if len(lines) != 0 {
		lines = append(lines, "")
	}
	if len(s.plan.Renames) == 0 {
		return strings.Join(append(lines, "nothing to rename"), "\n")
	}
	for _, i := range s.items() {
		line := s.plan.Line(i.r)
		if i.r.Skip == "" && !i.selected {
			line += " (unselected)"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// ask asks to rename the selected ones.
func (s *renameState) ask(ctx context.Context) tea.Cmd {
	if base.Busy(ctx) {
		return base.Notify(busyNotice)
	}
	renames := s.selected()
	folders, files := client.RenameCounts(renames)
	if folders == 0 && files == 0 {
		if !s.loaded {
			return nil
		}
		return base.Notify("Nothing selected to rename")
	}
	plan := s.plan
	req := s.opts
	req.Paths, req.Only = s.paths, []string{}
	for _, r := range renames {
		req.Only = append(req.Only, r.From)
	}
	var lines []string
	for _, r := range renames {
		lines = append(lines, plan.Line(r))
	}
	var what []string
	if folders != 0 {
		what = append(what, stringutil.Quantify(folders, "folder", "folders"))
	}
	if files != 0 {
		what = append(what, stringutil.Quantify(files, "chapter file", "chapter files"))
	}
	question := fmt.Sprintf("Rename %s? %s in the library undoes it.", strings.Join(what, " and "), base.KeyUndo.Help().Key)
	s.Confirm.Ask("Rename", strings.Join(append(lines, "", question), "\n"), s.View(), func() tea.Msg {
		return actionMsg{label: "Renaming", do: func(ctx context.Context, status func(string)) (reloadMsg, error) {
			// only the selected ones, planned again
			res, err := applyChange(ctx, req, status)
			if err != nil {
				return reloadMsg{}, err
			}
			var failed []string
			for _, l := range res.Lines {
				if l.Warn {
					failed = append(failed, strings.TrimPrefix(l.Text, "skipped "))
				}
			}
			done, _ := res.Detail.(client.RenameResult)
			return reloadMsg{notify: renamedNotice(done, failed)}, nil
		}}
	})
	return nil
}

// renamedNotice says what the rename did.
func renamedNotice(res client.RenameResult, failed []string) string {
	notify := "renamed " + stringutil.Quantify(res.Folders, "folder", "folders")
	if res.Files != 0 {
		notify += ", " + stringutil.Quantify(res.Files, "chapter file", "chapter files")
	}
	if res.Kept != 0 {
		notify += fmt.Sprintf(", %d kept (not empty)", res.Kept)
	}
	if len(failed) != 0 {
		notify += fmt.Sprintf(", %d failed (%s)", len(failed), failed[0])
	}
	return undoNotice(notify)
}

func (s *renameState) View() string {
	if s.Asking() {
		return s.Confirm.View()
	}
	if s.loaded && len(s.List.Items()) == 0 && s.List.Unfiltered() {
		return lipgloss.NewStyle().Foreground(color.Secondary).Render("Nothing to rename.")
	}
	return s.List.View()
}

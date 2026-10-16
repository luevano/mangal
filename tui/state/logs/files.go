// Package logs is the Logs screen: the current log (every file of the current period,
// followed), the past periods, and the log files (and this session's entries), each opened
// in a viewer with find, a level filter and follow, and a search through every file.
package logs

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	_list "charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/dustin/go-humanize"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/model/search"
	"github.com/luevano/mangal/v5/tui/util"
)

// item is a log file, or this session's entries when file is nil.
type item struct {
	file *log.File
}

func (i *item) ItemKey() string {
	if i.file == nil {
		return ""
	}
	return i.file.Name
}

func (i *item) FilterValue() string {
	if i.file == nil {
		return "this session"
	}
	return i.file.Name
}

func (i *item) Title() string {
	if i.file == nil {
		return "This session"
	}
	return i.file.Name
}

func (i *item) Description() string {
	if i.file == nil {
		return "the recent entries of this mangal, also the ones not written to a file"
	}
	f := i.file
	state := "open"
	if f.Closed {
		state = "closed"
	}
	return fmt.Sprintf("%s · last written %s · %s", humanize.Bytes(uint64(f.Size)), f.ModTime.Format("2006-01-02 15:04"), state)
}

// KeyView opens the selected file.
var KeyView = util.Bind("view", "enter")

type filesKeys struct {
	view, search, refresh key.Binding
}

var _ base.State = (*files)(nil)

// files is the list of the log files, newest first.
type files struct {
	list.Screen
	dir    string
	keys   filesKeys
	search *search.Model
	size   base.Size
}

// New is the Logs screen: the current log, this session's entries when the log file isn't
// written.
func New() base.State {
	dir := log.Dir()
	if dir == "" {
		dir = path.Logs()
	}
	return newMain(dir, log.FileError())
}

// newMain is the viewer ctrl+l opens: the current period of dir, this session's entries
// without a log file (fileErr).
func newMain(dir string, fileErr error) *viewer {
	if fileErr != nil {
		v := newSession()
		v.main, v.dir, v.note = true, dir, "no log file: "+fileErr.Error()
		return v
	}
	return newPeriod(dir, "")
}

func newFiles(dir string) *files {
	s := &files{
		dir:    dir,
		keys:   filesKeys{view: KeyView, search: base.KeySearch, refresh: base.KeyRefresh},
		search: search.New("Search every log file (regexp)...", "", 64, 0),
	}
	s.Screen = list.NewScreen(list.New(2, 1, "file", "files", []*item{}, func(i *item) _list.DefaultItem { return i }), s.keyMap)
	return s
}

func (s *files) keyMap() help.KeyMap {
	return util.KeyMap{Short: []key.Binding{s.keys.view, s.keys.search, s.keys.refresh}}
}

func (s *files) Key() string       { return "logs/files" }
func (s *files) Title() base.Title { return base.Title{Text: "Logs · files"} }

func (s *files) Subtitle() string {
	if s.dir == "" {
		return "no log dir"
	}
	return s.dir
}

func (s *files) Inputting() bool { return s.search.Searching() || s.Screen.Inputting() }
func (s *files) Backable() bool  { return !s.search.Searching() && s.Screen.Backable() }

func (s *files) KeyMap() help.KeyMap {
	if s.search.Searching() {
		return inputKeys("search")
	}
	return s.Screen.KeyMap()
}

func (s *files) Resize(size base.Size) tea.Cmd {
	s.size = size
	s.search.Resize(size)
	if s.search.Searching() {
		size.Height = max(1, size.Height-2)
	}
	return s.Screen.Resize(size)
}

// filesMsg are the files listed.
type filesMsg struct{ files []log.File }

func (s *files) load(ctx context.Context) tea.Cmd {
	dir := s.dir
	return base.Load(ctx, "", func(ctx context.Context) ([]log.File, error) {
		if dir == "" {
			return nil, nil
		}
		return logsOf(dir).Files(ctx)
	}, func(f []log.File) tea.Msg { return filesMsg{f} })
}

func (s *files) Init(ctx context.Context) tea.Cmd {
	s.setItems(nil)
	return s.load(ctx)
}

func (s *files) setItems(files []log.File) tea.Cmd {
	items := []_list.Item{&item{}}
	for _, f := range slices.Backward(files) {
		items = append(items, &item{file: &f})
	}
	cmd := s.List.SetItemsKeep(items)
	s.keys.view.SetEnabled(len(items) > 0)
	return cmd
}

func (s *files) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case filesMsg:
		return s.setItems(msg.files)
	case base.RestoredMsg:
		return s.load(ctx)
	case search.SearchMsg:
		s.Resize(s.size)
		return push(newSearch(s.dir, string(msg)))
	case search.SearchCancelMsg:
		s.Resize(s.size)
		return nil
	case tea.KeyPressMsg:
		if s.search.Searching() {
			return s.search.Update(msg)
		}
		if s.List.Filtering() {
			break
		}
		switch {
		case key.Matches(msg, s.keys.search):
			cmd := s.search.Focus()
			s.Resize(s.size)
			return cmd
		case key.Matches(msg, s.keys.refresh):
			return s.load(ctx)
		case key.Matches(msg, s.keys.view):
			i, ok := s.List.SelectedItem().(*item)
			if !ok {
				return nil
			}
			all := make([]log.File, 0, len(s.List.Items()))
			for _, it := range s.List.Items() {
				if f := it.(*item).file; f != nil {
					all = append(all, *f)
				}
			}
			slices.Reverse(all)
			return push(s.viewer(i.file, all))
		}
	}
	if !s.search.Searching() {
		return s.List.Update(msg)
	}
	return s.search.Update(msg)
}

// viewer is the viewer of f (this session's for nil), files are every file, oldest first.
func (s *files) viewer(f *log.File, files []log.File) *viewer {
	if f == nil {
		return newSession()
	}
	// the newest file follows: the next ones come after it
	newest := len(files) > 0 && files[len(files)-1].Name == f.Name && !f.Compressed
	return newFile(s.dir, *f, newest)
}

func (s *files) View() string {
	if !s.search.Searching() {
		return s.Screen.View()
	}
	return lipgloss.JoinVertical(lipgloss.Left, s.search.View(), "", s.Screen.View())
}

func push(state base.State) tea.Cmd {
	return func() tea.Msg { return state }
}

// sessionKey is the key part of this session's viewer.
const sessionKey = "session"

func viewerKey(name string) string {
	return "logs/" + strings.TrimSuffix(name, ".gz")
}

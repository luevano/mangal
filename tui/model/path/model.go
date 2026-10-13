// Package path shows mangal's paths (mangal path).
package path

import (
	"context"
	"fmt"
	"slices"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/util"
)

var _ base.State = (*state)(nil)

type state struct {
	table table.Model
	copy  key.Binding
}

// New is the paths screen.
func New() base.State {
	paths := path.All()
	keys := paths.Keys()
	slices.Sort(keys)
	rows := make([]table.Row, len(keys))
	for i, k := range keys {
		rows[i] = table.Row{string(k), paths.Get(k)}
	}

	_styles := defaultStyles()
	t := table.New(
		table.WithColumns([]table.Column{
			{Title: "Name", Width: 10},
			{Title: "Path", Width: 40},
		}),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(len(rows)+1),
		table.WithStyles(table.Styles{
			Header:   _styles.header,
			Cell:     _styles.cell,
			Selected: _styles.selected,
		}),
	)
	util.TableKeys(&t.KeyMap)
	return &state{
		table: t,
		copy:  util.Bind("copy", "c", "enter"),
	}
}

func (s *state) Backable() bool               { return true }
func (s *state) Inputting() bool              { return false }
func (s *state) Subtitle() string             { return "" }
func (s *state) Status() string               { return "" }
func (s *state) Init(context.Context) tea.Cmd { return nil }
func (s *state) View() string                 { return s.table.View() }

func (s *state) Key() string { return "path" }

func (s *state) Title() base.Title {
	return base.Title{Text: "Mangal Paths"}
}

func (s *state) KeyMap() help.KeyMap {
	return util.KeyMap{Short: []key.Binding{s.copy, s.table.KeyMap.LineUp, s.table.KeyMap.LineDown}}
}

func (s *state) Resize(size base.Size) tea.Cmd {
	cols := s.table.Columns()
	cols[1].Width = max(10, size.Width-cols[0].Width-4)
	s.table.SetColumns(cols)
	return nil
}

func (s *state) Update(_ context.Context, msg tea.Msg) tea.Cmd {
	if msg, ok := msg.(tea.KeyMsg); ok && key.Matches(msg, s.copy) {
		row := s.table.SelectedRow()
		if row == nil {
			return nil
		}
		return func() tea.Msg {
			if err := clipboard.WriteAll(row[1]); err != nil {
				return base.NotificationMsg{Message: "Copy: " + err.Error()}
			}
			return base.NotificationMsg{Message: fmt.Sprintf("Copied the %s path", row[0])}
		}
	}
	var cmd tea.Cmd
	s.table, cmd = s.table.Update(msg)
	return cmd
}

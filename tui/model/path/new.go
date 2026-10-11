package path

import (
	"slices"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/tui/model/help"
)

func New(standalone bool) *Model {
	cols := []table.Column{
		{
			Title: "Name",
			Width: 10,
		},
		{
			Title: "Path",
			Width: 40,
		},
	}

	paths := path.AllPaths()
	rows := make([]table.Row, len(paths))
	keys := paths.Keys()
	slices.Sort(keys)
	for i, k := range keys {
		rows[i] = table.Row{string(k), paths.Get(k)}
	}

	_styles := defaultStyles()
	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(len(rows)),
		table.WithStyles(table.Styles{
			Header:   _styles.header,
			Cell:     _styles.cell,
			Selected: _styles.selected,
		}),
	)

	m := &Model{
		table:                t,
		help:                 help.New(),
		standalone:           standalone,
		title:                _styles.title.Render("Mangal Paths"),
		notificationDuration: 2 * time.Second,
		styles:               _styles,
		keyMap:               newKeyMap(),
	}
	if !standalone {
		m.DisableQuitKeybindings()
	}
	return m
}

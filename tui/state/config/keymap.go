package config

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/luevano/mangal/v5/tui/util"
)

type keyMap struct {
	change,
	reset,
	save,
	cancel,
	add,
	editEntry,
	remove,
	up,
	down,
	close key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		change:    util.BindNamedKey("enter/space", "change", "enter", " "),
		reset:     util.Bind("reset to default", "r"),
		save:      util.Bind("save", "enter"),
		cancel:    util.Bind("cancel", "esc"),
		add:       util.Bind("add", "a"),
		editEntry: util.Bind("edit", "enter"),
		remove:    util.Bind("remove", "d"),
		up:        util.BindNamedKey("↑/k", "up", "up", "k"),
		down:      util.BindNamedKey("↓/j", "down", "down", "j"),
		close:     util.Bind("done", "esc"),
	}
}

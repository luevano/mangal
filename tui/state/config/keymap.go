package config

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/util"
)

type keyMap struct {
	expandAll,
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
		expandAll: util.Bind("expand/collapse all", "E"),
		change:    util.BindNamedKey("enter/space", "change", "enter", " "),
		reset:     util.Bind("reset to default", "r"),
		save:      util.Bind("save", "enter"),
		cancel:    util.Bind("cancel", "esc"),
		add:       util.Bind("add", "a"),
		editEntry: util.BindNamedKey("enter/e", "edit", "enter", "e"),
		remove:    base.KeyRemove,
		up:        util.BindNamedKey("↑/k", "up", "up", "k"),
		down:      util.BindNamedKey("↓/j", "down", "down", "j"),
		close:     util.Bind("done", "esc"),
	}
}

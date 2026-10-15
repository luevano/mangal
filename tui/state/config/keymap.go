package config

import (
	"charm.land/bubbles/v2/key"
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
		expandAll: base.KeyExpandAll,
		change:    base.KeyChange,
		reset:     base.KeyReset,
		save:      util.Bind("save", "enter"),
		cancel:    util.Bind("cancel", "esc"),
		add:       base.KeyAdd,
		editEntry: util.BindNamedKey("enter/e", "edit", "enter", "e"),
		remove:    base.KeyRemove,
		up:        base.KeyUp,
		down:      base.KeyDown,
		close:     util.Bind("done", "esc"),
	}
}

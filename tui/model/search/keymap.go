package search

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/luevano/mangal/v5/tui/util"
)

func newKeyMap() keyMap {
	return keyMap{
		confirm: util.Bind("confirm", "enter"),
		cancel:  util.Bind("cancel", "esc"),
	}
}

type keyMap struct {
	confirm,
	cancel key.Binding
}

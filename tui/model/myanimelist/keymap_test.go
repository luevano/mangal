package myanimelist

import (
	"testing"

	"github.com/luevano/mangal/v5/tui/base"
)

// TestKeys: the keymap has every mode's keys (enter and esc mean one thing per mode), only
// the base keys are compared.
func TestKeys(t *testing.T) {
	k := newKeyMap()
	if keys := base.KeyInconsistencies(&k); len(keys) != 0 {
		t.Error(keys)
	}
}

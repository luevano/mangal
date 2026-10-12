package home

import (
	"testing"

	"github.com/luevano/mangal/v5/tui/base"
)

func TestKeys(t *testing.T) {
	if keys := base.KeyCollisions(New().KeyMap()); len(keys) != 0 {
		t.Error(keys)
	}
}

package format

import (
	"testing"

	"github.com/luevano/mangal/v5/tui/base"
)

func TestKeys(t *testing.T) {
	k := newKeyMap()
	if keys := base.KeyCollisions(k); len(keys) != 0 {
		t.Error(keys)
	}
}

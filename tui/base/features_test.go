package base

import (
	"slices"
	"testing"

	"charm.land/bubbles/v2/key"
	"github.com/luevano/mangal/v5/core"
)

// every feature's TUI key is a Key* binding's
func TestFeatureKeys(t *testing.T) {
	old, newer := Keys()
	all := append(old, newer...)
	for _, f := range core.Features {
		if f.TUIKey == "" {
			continue
		}
		if !slices.ContainsFunc(all, func(b key.Binding) bool { return slices.Contains(b.Keys(), f.TUIKey) }) {
			t.Errorf("%s: no Key* binding has %q", f.Name, f.TUIKey)
		}
	}
}

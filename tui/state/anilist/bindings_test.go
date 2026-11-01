package anilist

import (
	"github.com/luevano/mangal/v5/tui/base"
	"testing"

	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/storage"
)

func TestSourceItem(t *testing.T) {
	src := client.MetadataSources[1]
	i := &sourceItem{src: src}
	if i.Title() != "Kitsu" || i.Description() != "not bound" {
		t.Errorf("unbound: %q, %q", i.Title(), i.Description())
	}
	i.binding = &storage.Binding{Source: src.Code, ID: "38", Default: true}
	if i.Title() != "Kitsu (default)" || i.Description() != "38" {
		t.Errorf("bound: %q, %q", i.Title(), i.Description())
	}
	i.binding.Default = false
	if i.Title() != "Kitsu (backup)" {
		t.Errorf("backup: %q", i.Title())
	}
}

// TestKeys: no key means two things on the screen (its keys, the list's, the global ones).
func TestKeys(t *testing.T) {
	s := newBindings(nil, nil).(*bindingsState)
	if keys := base.KeyCollisions(s.keyMap(), s.List.KeyMap.Browsing()...); len(keys) != 0 {
		t.Error(keys)
	}
}

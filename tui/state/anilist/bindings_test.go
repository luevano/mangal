package anilist

import (
	"strings"
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
}

func TestIDQuery(t *testing.T) {
	for q, want := range map[string]string{"38": "38", " id: 30013 ": "30013", "One Piece": "", "id:x": "", "20th Century Boys": ""} {
		got := ""
		if m := idQuery.FindStringSubmatch(q); m != nil {
			got = m[1]
		}
		if got != want {
			t.Errorf("%q: %q, want %q", q, got, want)
		}
	}
	if !strings.HasPrefix(client.MetadataSources[0].URL, "https://anilist.co/") {
		t.Error("anilist url")
	}
}

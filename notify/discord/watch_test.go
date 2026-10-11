package discord

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/storage"
)

func TestWatchEmbed(t *testing.T) {
	from := float32(65)
	r := watch.Result{WatchRun: storage.WatchRun{Provider: "mango-mangaplus", Title: "Dandadan", From: &from, Status: storage.StatusNew,
		Chapters: []storage.RunChapter{{Number: 65.5, Title: "extra", Path: "/lib/Dandadan/a.cbz"}, {Number: 70, Path: "/lib/Dandadan/b.cbz"}}}}
	e := watchEmbed(r, true)
	if e.Description != "chapters 65 → 70: 2 new (from 65)" || e.Title != "Dandadan" || e.Color != ColorGreen {
		t.Errorf("embed %q %q %d", e.Title, e.Description, e.Color)
	}
	last := e.Fields[len(e.Fields)-1]
	if last.Name != "chapters" || last.Value != "/lib/Dandadan:\n> 65.5 - extra\n> 70" {
		t.Errorf("chapters field %q", last.Value)
	}

	r.From, r.Error = nil, "chapter 71: boom"
	if e := watchEmbed(r, false); e.Description != "chapters → 70: 2 new (first check)" || e.Color != ColorOrange || e.Fields[len(e.Fields)-1].Name != "failed" {
		t.Errorf("first check with a failure: %+v", e)
	}
	if e := watchErrorEmbed(watch.Result{WatchRun: storage.WatchRun{Provider: "p", Title: "X", Error: strings.Repeat("e", 5000)}}); utf8.RuneCountInString(e.Description) > maxDescription || e.Color != ColorRed {
		t.Errorf("error embed too long or not red: %d", len(e.Description))
	}
}

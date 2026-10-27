package history

import (
	"github.com/luevano/mangal/v5/tui/util"

	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/tui/base"
)

func TestKeys(t *testing.T) {
	s := New().(*state)
	if keys := base.KeyCollisions(s.keyMap(), s.List.KeyMap.Browsing()...); len(keys) != 0 {
		t.Error(keys)
	}
	if bad := base.KeyInconsistencies(s.keyMap()); len(bad) != 0 {
		t.Error(bad)
	}
}

// d forgets the manga's reads (asks with the plan, d or y), the undo brings them back
func TestClear(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	data, err := storage.OpenData(filepath.Join(dir, storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { data.Close() })
	history := storage.History{DB: data, User: storage.DefaultUser}
	for _, r := range []storage.Read{{Provider: "p", MangaID: "a", MangaTitle: "A", Chapter: 1}, {Provider: "p", MangaID: "a", MangaTitle: "A", Chapter: 2}, {Provider: "q", MangaID: "b", MangaTitle: "B", Chapter: 1}} {
		if err := history.Record(r); err != nil {
			t.Fatal(err)
		}
	}
	c, err := core.New(ctx, core.Env{Storage: storage.Storage{Data: data}, BackupsDir: filepath.Join(dir, "backups"), Root: func() string { return dir }})
	if err != nil {
		t.Fatal(err)
	}
	old := base.Core
	t.Cleanup(func() { base.Core = old })
	base.Core = func() (*core.Core, error) { return c, nil }

	s := New().(*state)
	s.Resize(base.Size{Width: 100, Height: 30})
	run := func(msgs ...tea.Msg) {
		for len(msgs) != 0 {
			msg := msgs[0]
			msgs = append(msgs[1:], base.Drain(s.Update(ctx, msg))...)
		}
	}
	run(base.Drain(s.Init(ctx))...)
	if n := len(s.List.Items()); n != 2 {
		t.Fatalf("%d mangas", n)
	}
	s.List.Select(0)
	selected := s.List.SelectedItem().(*item)
	run(util.KeyPress("d"))
	if !s.Confirm.Active() || !strings.Contains(s.Confirm.View(), "forget") {
		t.Fatalf("no question:\n%s", s.Confirm.View())
	}
	run(util.KeyPress("y"))
	if reads, _ := history.List(selected.provider, selected.mangaID); len(reads) != 0 {
		t.Fatalf("not forgotten: %+v", reads)
	}
	if n := len(s.List.Items()); n != 1 {
		t.Errorf("%d mangas after", n)
	}
	if ops, _ := c.Library.Ops(ctx, 0); len(ops) != 1 || ops[0].Kind != "history-clear" {
		t.Errorf("ops %+v", ops)
	}
}

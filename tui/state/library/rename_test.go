package library

import (
	"github.com/luevano/mangal/v5/tui/util"

	"context"
	"slices"
	"strings"
	"testing"

	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/tui/base"
)

// the screen shows library rename's plan, the renames selected but the skipped ones; enter
// asks, yes renames the selected ones only (in the plan's order), a new plan keeps what
// was unselected
func TestRename(t *testing.T) {
	ctx := context.Background()
	var applied []core.RenameReq
	old := applyVariant
	t.Cleanup(func() { applyVariant = old })
	applyVariant = func(_ context.Context, _ core.Plan, ch core.Change, _ func(string)) (core.OpResult, error) {
		req := ch.(core.RenameReq)
		applied = append(applied, req)
		return core.OpResult{Detail: client.RenameResult{Folders: len(req.Only)}}, nil
	}
	planned := func(chapters bool) renamePlannedMsg {
		plan := client.RenamePlan{Root: "/dl", Unmarked: 2, Renames: []library.Rename{
			{From: "/dl/Berserk", To: "/dl/Berserk (1989)"},
			{From: "/dl/Vagabond", To: "/dl/Vagabond (1998)"},
			{From: "/dl/Kagurabachi", To: "/dl/Kagurabachi", Skip: "no metadata", NoMetadata: true},
		}}
		if chapters {
			plan.Renames = append([]library.Rename{{From: "/dl/Berserk/1.cbz", To: "/dl/Berserk/[1] The Black Swordsman.cbz", Chapter: true}}, plan.Renames...)
		}
		return renamePlannedMsg{plan: plan}
	}

	s := newRenameState(nil)
	s.Resize(base.Size{Width: 120, Height: 40})
	s.Update(ctx, planned(false))
	items := s.items()
	if len(items) != 3 || !items[0].selected || !items[1].selected || items[2].selected {
		t.Fatalf("items %+v", items)
	}
	if !strings.Contains(s.Status(), "2 selected, 1 skipped") || !strings.Contains(s.Status(), "2 notes") {
		t.Errorf("status %q", s.Status())
	}
	if !strings.Contains(items[2].Description(), "alt+b renames it") {
		t.Errorf("no metadata: %q", items[2].Description())
	}

	// the skipped one can't be selected; Berserk unselected
	s.List.Select(2)
	s.Update(ctx, util.KeyPress("space"))
	if items[2].selected {
		t.Error("a skipped rename selected")
	}
	s.List.Select(0)
	s.Update(ctx, util.KeyPress("space"))
	if items[0].selected {
		t.Fatal("space didn't unselect")
	}

	s.Update(ctx, util.KeyPress("enter"))
	if !s.Confirm.Active() {
		t.Fatal("enter didn't ask")
	}
	question := strings.Join(strings.Fields(s.Confirm.View()), " ")
	if !strings.Contains(question, "Vagabond -> Vagabond (1998)") || strings.Contains(question, "Berserk ->") || !strings.Contains(question, "Rename 1 folder?") {
		t.Errorf("question:\n%s", s.Confirm.View())
	}
	act, ok := s.Update(ctx, press("y"))().(actionMsg)
	if !ok {
		t.Fatal("yes isn't the rename")
	}
	reload, err := act.do(ctx, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || !slices.Equal(applied[0].Only, []string{"/dl/Vagabond"}) || !strings.Contains(reload.notify, "renamed 1 folder") || !strings.Contains(reload.notify, "U undoes it") {
		t.Errorf("applied %+v, notify %q", applied, reload.notify)
	}

	// alt+f plans the chapter files too, Berserk stays unselected
	s.Update(ctx, util.KeyPress("alt+f"))
	if !s.opts.Chapters || !strings.Contains(s.Subtitle(), "chapter files on") {
		t.Fatalf("alt+f: %+v, %q", s.opts, s.Subtitle())
	}
	s.Update(ctx, planned(true))
	items = s.items()
	if len(items) != 4 || !items[0].r.Chapter || !items[0].selected || items[1].selected || !items[2].selected {
		t.Fatalf("with the chapters %+v", items)
	}
	// a selects all, the chapter file goes first
	s.Update(ctx, press("a"))
	s.Update(ctx, util.KeyPress("enter"))
	act = s.Update(ctx, press("y"))().(actionMsg)
	if _, err := act.do(ctx, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if last := applied[len(applied)-1]; len(last.Only) != 3 || last.Only[0] != "/dl/Berserk/1.cbz" || !last.Chapters || last.Fallback || last.Refresh {
		t.Errorf("applied %+v", last)
	}

	// backspace unselects all: nothing to ask
	s.Update(ctx, util.KeyPress("backspace"))
	s.Update(ctx, util.KeyPress("enter"))
	if s.Confirm.Active() {
		t.Error("asked with nothing selected")
	}
}

// a health finding's paths are its own window
func TestRenamePaths(t *testing.T) {
	if s := newRenameState([]string{"/dl/MangaDex"}); s.Key() == newRenameState(nil).Key() {
		t.Error("a finding's renames are the whole library's window")
	}
}

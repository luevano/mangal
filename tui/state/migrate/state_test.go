package migrate

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/tui/base"
)

// a plan longer than the screen scrolls (MT-A8)
func TestPlanScrolls(t *testing.T) {
	s := New().(*state)
	s.Resize(base.Size{Width: 80, Height: 10})
	var markers []library.OldMarker
	for i := range 40 {
		markers = append(markers, library.OldMarker{Dir: fmt.Sprintf("manga %02d", i), Old: "old", New: "new"})
	}
	ctx := context.Background()
	s.Update(ctx, plannedMsg{plan: core.Plan{Lines: library.Migration{Markers: markers}.Lines()}})
	if v := s.View(); !strings.Contains(v, "manga 00") || strings.Contains(v, "manga 39") {
		t.Fatalf("view:\n%s", v)
	}
	s.Update(ctx, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if v := s.View(); !strings.Contains(v, "manga 39") {
		t.Errorf("G didn't scroll to the end:\n%s", v)
	}
	if !s.keyMap.apply.Enabled() {
		t.Error("enter can't migrate")
	}
}

func TestNothingToMigrate(t *testing.T) {
	s := New().(*state)
	s.Resize(base.Size{Width: 80, Height: 10})
	ctx := context.Background()
	if s.View() != "" {
		t.Errorf("before the plan: %q", s.View())
	}
	s.Update(ctx, plannedMsg{})
	if !strings.Contains(s.View(), "Nothing to migrate") || s.keyMap.apply.Enabled() {
		t.Errorf("empty plan: enter %v\n%s", s.keyMap.apply.Enabled(), s.View())
	}
	if cmd := s.Update(ctx, tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Error("enter migrates nothing")
	}
}

// after the migration it goes back, saying how to re-link the watches that need it
func TestMigrated(t *testing.T) {
	s := New().(*state)
	ctx := context.Background()
	for relink, want := range map[int]string{0: "folders keep their names", 2: "2 watches need a re-link"} {
		msgs := base.Drain(s.Update(ctx, migratedMsg{relink: relink}))
		if len(msgs) != 2 {
			t.Fatalf("relink %d: %v", relink, msgs)
		}
		if _, ok := msgs[0].(base.BackMsg); !ok {
			t.Errorf("relink %d: %T, want back", relink, msgs[0])
		}
		var notice string
		switch n := msgs[1].(type) {
		case base.NotificationMsg:
			notice = n.Message
		case base.NotificationWithDurationMsg:
			notice = n.Message
		}
		if !strings.Contains(notice, want) {
			t.Errorf("relink %d: %q", relink, notice)
		}
	}
}

func TestKeys(t *testing.T) {
	s := New().(*state)
	if keys := base.ViewerKeyCollisions(s.KeyMap()); len(keys) != 0 {
		t.Error(keys)
	}
}

package migrate

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
	s.Update(ctx, plannedMsg{migration: library.Migration{Markers: markers}})
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

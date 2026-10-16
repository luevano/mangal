package downloads

import (
	"github.com/luevano/mangal/v5/tui/util"

	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/tui/base"
	tuijobs "github.com/luevano/mangal/v5/tui/jobs"
)

func withManager(t *testing.T) *jobs.Manager {
	t.Helper()
	m := jobs.New(context.Background())
	tuijobs.SetManager(m)
	t.Cleanup(func() {
		m.Shutdown(time.Second)
		tuijobs.SetManager(nil)
	})
	return m
}

// blocked is a job that runs until ctx is cancelled or the test ends.
func blocked(t *testing.T, title, origin string, providers ...string) jobs.Spec {
	end := make(chan struct{})
	t.Cleanup(func() { close(end) })
	return jobs.Spec{Kind: jobs.KindDownload, Title: title, Origin: origin, Providers: providers, Run: func(ctx context.Context, p *jobs.Progress) error {
		p.Running("downloading " + title)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-end:
			return nil
		}
	}}
}

func waitStatus(t *testing.T, m *jobs.Manager, id int64, status jobs.Status) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		changed := m.Changed()
		if j, _ := m.Get(id); j.Status == status {
			return
		}
		select {
		case <-changed:
		case <-deadline:
			j, _ := m.Get(id)
			t.Fatalf("job %d is %s, want %s", id, j.Status, status)
		}
	}
}

func titles(s *state) []string {
	var list []string
	for _, i := range s.List.Items() {
		list = append(list, i.(*item).job.Title)
	}
	return list
}

func press(k string) tea.KeyPressMsg { return util.KeyPress(k) }

func TestGroupedByQueue(t *testing.T) {
	m := withManager(t)
	a := m.Add(blocked(t, "A", "chapters/p/1", "p"))
	m.Add(blocked(t, "B", "", "q"))
	c := m.Add(blocked(t, "C", "", "p"))
	m.Add(blocked(t, "D", "", "q"))
	done := m.Add(jobs.Spec{Kind: jobs.KindDownload, Title: "old", Providers: []string{"r"}})
	waitStatus(t, m, a.ID, jobs.Running)
	waitStatus(t, m, done.ID, jobs.Done)

	s := New().(*state)
	ctx := context.Background()
	s.Init(ctx)
	s.Resize(base.Size{Width: 120, Height: 40})
	if j, _ := s.selected(); j.Title != "D" {
		t.Errorf("selected %q, want the newest active job", j.Title)
	}
	s.Update(ctx, press("g"))
	want := []string{"A", "C", "B", "D", "old"}
	if got := titles(s); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("order %q, want %q", got, want)
	}
	if sub := s.Subtitle(); !strings.Contains(sub, "2 running") || !strings.Contains(sub, "p 1 queued") || !strings.Contains(sub, "q 1 queued") {
		t.Errorf("subtitle %q", sub)
	}
	view := ansi.Strip(s.View())
	if !strings.Contains(view, "queued, 1 ahead (behind A)") || !strings.Contains(view, "downloading A") {
		t.Errorf("view:\n%s", view)
	}

	// o goes to the screen that started it
	cmd := s.Update(ctx, press("o"))
	if cmd == nil {
		t.Fatal("o did nothing")
	}
	// backspace clears p's queue, not the running one
	s.Update(ctx, util.KeyPress("backspace"))
	waitStatus(t, m, c.ID, jobs.Cancelled)
	if j, _ := m.Get(a.ID); j.Status != jobs.Running {
		t.Errorf("A is %s", j.Status)
	}
	// d cancels the running one
	s.Update(ctx, press("d"))
	waitStatus(t, m, a.ID, jobs.Cancelled)
	s.Update(ctx, tuijobs.ChangedMsg{})
	if titles(s)[0] != "B" {
		t.Errorf("order after cancelling %q", titles(s))
	}

	// alt+d asks, then cancels everything
	s.Update(ctx, util.KeyPress("alt+d"))
	if !s.Asking() {
		t.Fatal("alt+d didn't ask")
	}
	confirm := s.Update(ctx, press("y"))
	for _, msg := range base.Drain(confirm) {
		s.Update(ctx, msg)
	}
	if len(m.Active()) != 0 {
		// running jobs stop in the background
		for _, j := range m.Active() {
			waitStatus(t, m, j.ID, jobs.Cancelled)
		}
	}
}

func TestRemoveFinished(t *testing.T) {
	m := withManager(t)
	done := m.Add(jobs.Spec{Kind: jobs.KindDownload, Title: "old", Providers: []string{"r"}})
	waitStatus(t, m, done.ID, jobs.Done)
	s := New().(*state)
	ctx := context.Background()
	s.Init(ctx)
	if s.keys.cancel.Help().Desc != "remove" {
		t.Errorf("d on a finished job: %q", s.keys.cancel.Help().Desc)
	}
	s.Update(ctx, press("d"))
	if len(s.List.Items()) != 0 || len(m.Jobs()) != 0 {
		t.Error("d didn't remove the finished job")
	}
}

func TestKeys(t *testing.T) {
	m := withManager(t)
	m.Add(blocked(t, "A", "", "p"))
	s := New().(*state)
	for _, finished := range []bool{false, true} {
		if finished {
			m.CancelAll()
			for _, j := range m.Jobs() {
				waitStatus(t, m, j.ID, jobs.Cancelled)
			}
			s.Update(context.Background(), tuijobs.ChangedMsg{})
		}
		if c := base.KeyCollisions(s.keyMap(), s.List.KeyMap.Browsing()...); len(c) != 0 {
			t.Errorf("finished %v: %q", finished, c)
		}
	}
}

// A job added while the screen isn't shown is selected when it's shown again: the batch the
// user just started (the chapters' notice says Q, enter).
func TestSelectsTheNewJob(t *testing.T) {
	m := withManager(t)
	m.Add(blocked(t, "A", "", "p"))
	s := New().(*state)
	ctx := context.Background()
	s.Init(ctx)
	m.Add(blocked(t, "B", "", "p"))
	// shown: the cursor stays
	s.Update(ctx, tuijobs.ChangedMsg{})
	if j, _ := s.selected(); j.Title != "A" {
		t.Errorf("selected %q while shown, want A", j.Title)
	}
	m.Add(blocked(t, "C", "", "q"))
	s.Update(ctx, base.RestoredMsg{})
	if j, _ := s.selected(); j.Title != "C" {
		t.Errorf("selected %q, want the new job C", j.Title)
	}
	cmd := s.Update(ctx, util.KeyPress("enter"))
	if cmd == nil {
		t.Fatal("enter did nothing")
	}
	if v, ok := cmd().(base.Keyed); !ok || !strings.HasPrefix(v.Key(), "download/") {
		t.Errorf("enter opened %#v", v)
	}
}

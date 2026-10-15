package download

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/jobs"
	"github.com/luevano/mangal/v5/tui/base"
	tuijobs "github.com/luevano/mangal/v5/tui/jobs"
	"github.com/luevano/mangal/v5/tui/util"
)

type fakeChapter struct {
	number float32
}

func (c fakeChapter) String() string { return fmt.Sprintf("Chapter %v", c.number) }
func (c fakeChapter) Info() mangadata.ChapterInfo {
	return mangadata.ChapterInfo{Number: c.number, Title: c.String()}
}
func (fakeChapter) Volume() mangadata.Volume { return nil }

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

func press(s string) tea.KeyPressMsg { return util.KeyPress(s) }

func TestJobView(t *testing.T) {
	m := withManager(t)
	ctx := context.Background()
	gate, first := make(chan struct{}), make(chan struct{})
	blocker := m.Add(jobs.Spec{Kind: jobs.KindCheck, Title: "check of p", Providers: []string{"p"}, Run: func(ctx context.Context, p *jobs.Progress) error {
		close(first)
		<-gate
		return nil
	}})
	<-first
	chapters := []mangadata.Chapter{fakeChapter{1}, fakeChapter{2}, fakeChapter{3}}
	fail := true
	job := m.Add(jobs.Spec{Kind: jobs.KindDownload, Title: "Manga: 3 chapters", Providers: []string{"p"}, MangaID: "m", Chapters: chapters, Run: func(ctx context.Context, p *jobs.Progress) error {
		return jobs.RunDownload(ctx, p, func(ctx context.Context, list []mangadata.Chapter, onEvent func(client.Event)) client.Results {
			results := client.NewResults(list)
			for i, r := range results {
				onEvent(client.Event{Kind: client.EventStart, Index: i, Result: r})
				switch {
				case r.Chapter.Info().Number == 2 && fail:
					r.Err = errors.New("boom")
				case r.Chapter.Info().Number == 3:
					r.Unavailable = "paid"
				default:
					r.Down = &metadata.DownloadedChapter{Directory: "/dl/manga"}
				}
				onEvent(client.Event{Kind: client.EventDone, Index: i, Result: r})
			}
			return results
		})
	}})

	s := New(job.ID).(*state)
	s.Init(ctx)
	s.Resize(base.Size{Width: 100, Height: 30})
	if s.Key() != fmt.Sprintf("download/%d", job.ID) || !s.Backable() {
		t.Errorf("key %q, backable %v", s.Key(), s.Backable())
	}
	if got := ansi.Strip(s.View()); !strings.Contains(got, "Queued behind check of p") || !strings.Contains(got, "1 job ahead on p") {
		t.Errorf("queued view:\n%s", got)
	}
	if !s.cancel.Enabled() || s.retry.Enabled() {
		t.Errorf("queued: cancel %v retry %v", s.cancel.Enabled(), s.retry.Enabled())
	}

	close(gate)
	waitStatus(t, m, blocker.ID, jobs.Done)
	waitStatus(t, m, job.ID, jobs.Failed)
	s.Update(ctx, tuijobs.ChangedMsg{})
	got := ansi.Strip(s.View())
	for _, want := range []string{"1 chapter failed: boom", "Succeeded:", "Failed:", "Unavailable:"} {
		if !strings.Contains(got, want) {
			t.Errorf("results view lacks %q:\n%s", want, got)
		}
	}
	if s.cancel.Enabled() || !s.retry.Enabled() || !s.open.Enabled() {
		t.Errorf("over: cancel %v retry %v open %v", s.cancel.Enabled(), s.retry.Enabled(), s.open.Enabled())
	}

	// r queues the failed chapter again
	fail = false
	s.Update(ctx, press("r"))
	waitStatus(t, m, job.ID, jobs.Done)
	s.Update(ctx, tuijobs.ChangedMsg{})
	if n := len(s.job.Results.Succeed()); n != 2 {
		t.Errorf("%d succeeded after the retry, want 2", n)
	}
}

func TestJobViewCancel(t *testing.T) {
	m := withManager(t)
	ctx := context.Background()
	started := make(chan struct{})
	job := m.Add(jobs.Spec{Kind: jobs.KindDownload, Title: "Manga: chapter 1", Providers: []string{"p"}, Chapters: []mangadata.Chapter{fakeChapter{1}}, Run: func(ctx context.Context, p *jobs.Progress) error {
		p.Running("downloading chapter 1 (1/1)")
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}})
	<-started
	s := New(job.ID).(*state)
	s.Init(ctx)
	s.Resize(base.Size{Width: 100, Height: 30})
	if got := ansi.Strip(s.View()); !strings.Contains(got, "Downloading chapter 1 (1/1)") {
		t.Errorf("running view:\n%s", got)
	}
	s.Update(ctx, press("d"))
	waitStatus(t, m, job.ID, jobs.Cancelled)
	s.Update(ctx, tuijobs.ChangedMsg{})
	if got := ansi.Strip(s.View()); !strings.Contains(got, "Cancelled.") {
		t.Errorf("cancelled view:\n%s", got)
	}

	// removed from the list meanwhile
	m.Remove(job.ID)
	s.Update(ctx, base.RestoredMsg{})
	if got := ansi.Strip(s.View()); !strings.Contains(got, "isn't listed anymore") {
		t.Errorf("gone view:\n%s", got)
	}
}

func TestKeys(t *testing.T) {
	withManager(t)
	s := New(1).(*state)
	if c := base.KeyCollisions(util.KeyMap{Short: []key.Binding{s.cancel, s.open, s.retry}}); len(c) != 0 {
		t.Errorf("collisions: %q", c)
	}
	if c := base.ViewerKeyCollisions(s.KeyMap()); len(c) != 0 {
		t.Errorf("collisions with the pager: %q", c)
	}
}

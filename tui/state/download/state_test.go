package download

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
)

type fakeChapter struct {
	number float32
}

func (c fakeChapter) String() string { return fmt.Sprintf("Chapter %v", c.number) }
func (c fakeChapter) Info() mangadata.ChapterInfo {
	return mangadata.ChapterInfo{Number: c.number, Title: c.String()}
}
func (fakeChapter) Volume() mangadata.Volume { return nil }

func done(run, index int, r client.Result) eventMsg {
	return eventMsg{run: run, kind: client.EventDone, index: index, result: r}
}

func TestDownloadEvents(t *testing.T) {
	chapters := []mangadata.Chapter{fakeChapter{1}, fakeChapter{2}, fakeChapter{3}}
	s := New(nil, chapters, libmangal.DownloadOptions{}).(*state)
	ctx := context.Background()
	s.start(ctx, s.results.ToDownload())
	run := s.run
	down := &metadata.DownloadedChapter{Directory: "/dl/manga"}

	s.Update(ctx, eventMsg{run: run, kind: client.EventStart, index: 0})
	s.Update(ctx, done(run, 0, client.Result{Chapter: chapters[0], Err: errors.New("boom")}))
	s.Update(ctx, eventMsg{run: run, kind: client.EventStart, index: 1})
	s.Update(ctx, eventMsg{run: run, kind: client.EventWait, index: 1, attempt: 2, wait: time.Second})
	if got := ansi.Strip(s.viewSummary()); !strings.Contains(got, `Retrying chapter 2 "Chapter 2"`) || !strings.Contains(s.message, "retry #2") {
		t.Errorf("waiting: summary %q, message %q", got, s.message)
	}
	s.Update(ctx, eventMsg{run: run, kind: client.EventProgress, index: 1, progress: &libmangal.DownloadProgress{Stage: libmangal.DownloadStageImages, Done: 23, Total: 57}})
	s.Update(ctx, eventMsg{run: run, kind: eventStatus, status: "MangaDex: rate limited"})
	if got := ansi.Strip(s.viewDownloading()); !strings.Contains(got, "23/57 pages") || !strings.Contains(got, "MangaDex: rate limited") {
		t.Errorf("progress: %q", got)
	}
	s.Update(ctx, done(run, 1, client.Result{Chapter: chapters[1], Down: down}))
	s.Update(ctx, done(run, 2, client.Result{Chapter: chapters[2], Unavailable: "paid"}))
	s.Update(ctx, finishedMsg{run: run})

	if s.downloading {
		t.Fatal("still downloading")
	}
	if !s.open.Enabled() || !s.retry.Enabled() {
		t.Errorf("open %v retry %v, want both", s.open.Enabled(), s.retry.Enabled())
	}
	// the first chapter failed (A9)
	if s.openCmd() == nil {
		t.Error("nothing to open")
	}

	// a retry downloads the failed one, its events index the retried chapters
	s.start(ctx, s.results.Failed())
	if s.run == run || len(s.batch) != 1 || s.results[0].Failed() {
		t.Fatalf("retry: run %d, batch %d, first failed %v", s.run, len(s.batch), s.results[0].Failed())
	}
	s.Update(ctx, done(run, 0, client.Result{Chapter: chapters[2]}))
	if s.done != 0 {
		t.Error("an event of the previous run was applied")
	}
	s.Update(ctx, eventMsg{run: s.run, kind: client.EventWait, index: 0, attempt: 1, wait: time.Second})
	if got := ansi.Strip(s.viewSummary()); !strings.Contains(got, `chapter 1 "Chapter 1"`) || !strings.Contains(got, "1/1") {
		t.Errorf("retry summary %q, want chapter 1", got)
	}
	s.Update(ctx, done(s.run, 0, client.Result{Chapter: chapters[0], Down: down}))
	s.Update(ctx, finishedMsg{run: s.run})
	if got := len(s.results.Succeed()); got != 2 {
		t.Errorf("%d succeeded, want 2", got)
	}
	if s.retry.Enabled() {
		t.Error("retry enabled without failures")
	}
}

func TestOpenWithoutDownloads(t *testing.T) {
	s := New(nil, []mangadata.Chapter{fakeChapter{1}}, libmangal.DownloadOptions{}).(*state)
	s.results[0].Err = errors.New("boom")
	if s.openCmd() != nil {
		t.Error("open without a downloaded chapter")
	}
}

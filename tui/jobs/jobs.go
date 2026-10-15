// Package jobs is the TUI's background downloads and checks: one client/jobs.Manager for
// the process, its line in the header, its notices, the quit question (base.Activity) and
// the marks of the lists.
package jobs

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client/jobs"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/notify"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/tui/base"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

var (
	mu      sync.Mutex
	manager *jobs.Manager

	// quiet are the jobs whose screen told the user they were added, the activity skips
	// their added notice. quietMu is held across the Add, so the activity can't see the
	// event before the id is here.
	quietMu sync.Mutex
	quiet   = map[int64]bool{}
)

// Start makes the process's manager, its jobs run with ctx; it's the TUI's base.Activity.
func Start(ctx context.Context) base.Activity {
	mu.Lock()
	defer mu.Unlock()
	manager = jobs.New(ctx)
	return &activity{m: manager}
}

// Manager is the process's manager (one with a background ctx if Start wasn't called, for
// tests and the standalone screens).
func Manager() *jobs.Manager {
	mu.Lock()
	defer mu.Unlock()
	if manager == nil {
		manager = jobs.New(context.Background())
	}
	return manager
}

// SetManager replaces the manager, for tests.
func SetManager(m *jobs.Manager) {
	mu.Lock()
	defer mu.Unlock()
	manager = m
}

// Download adds the download of the chapters of the manga as a job: origin is the window
// key of the screen that starts it, silent if it tells the user itself (no "downloading"
// notice). A desktop notification goes when it ends (notification.tui).
func Download(c *libmangal.Client, manga mangadata.Manga, chapters []mangadata.Chapter, options libmangal.DownloadOptions, read bool, origin string, silent bool) jobs.Job {
	title := manga.String() + ": " + chaptersTitle(chapters)
	spec := jobs.DownloadSpec(c, manga, chapters, options, title, origin)
	spec.Read = read
	if options.Directory == path.Temp() {
		run := spec.Run
		spec.Run = func(ctx context.Context, p *jobs.Progress) error {
			if _, err := path.EnsureTemp(); err != nil {
				return err
			}
			return run(ctx, p)
		}
	}
	spec.Finally = func(ctx context.Context, j jobs.Job) {
		if j.Status == jobs.Cancelled || read || !config.Notification.TUI.Get() {
			return
		}
		if err := notify.SendContext(ctx, j.Results); err != nil {
			log.Warn("send the notification: %s", err)
		}
	}
	if !silent {
		return Manager().Add(spec)
	}
	m := Manager()
	quietMu.Lock()
	defer quietMu.Unlock()
	job := m.Add(spec)
	quiet[job.ID] = true
	return job
}

// silenced is whether the job's added notice is skipped (once).
func silenced(id int64) bool {
	quietMu.Lock()
	defer quietMu.Unlock()
	ok := quiet[id]
	delete(quiet, id)
	return ok
}

func chaptersTitle(chapters []mangadata.Chapter) string {
	if len(chapters) == 1 {
		return "chapter " + stringutil.FormatFloat32(chapters[0].Info().Number)
	}
	return stringutil.Quantify(len(chapters), "chapter", "chapters")
}

// Check adds a check of the watches with the ids (of the providers) through m as a job.
func Check(m watch.Manager, ids []int64, providers []string, opts watch.CheckOptions, title, origin string, summary func(watch.Report) string) jobs.Job {
	return Manager().Add(jobs.CheckSpec(m, ids, providers, opts, title, origin, summary))
}

// ChangedMsg is jobs changing: Finished are the ones over since the last one. Every
// screen gets it while shown (base.Activity).
type ChangedMsg struct {
	Finished []jobs.Job
	notice   string
}

func (m ChangedMsg) Notice() string { return m.notice }

// FinishedFor is whether a download of the provider's manga is among the finished ones.
func (m ChangedMsg) FinishedFor(provider, mangaID string) bool {
	for _, j := range m.Finished {
		if j.Kind == jobs.KindDownload && j.MangaID == mangaID && len(j.Providers) == 1 && j.Providers[0] == provider {
			return true
		}
	}
	return false
}

type activity struct {
	m    *jobs.Manager
	seen uint64
}

// coalesce is how long changes are gathered into one msg (a page progress is many).
const coalesce = 100 * time.Millisecond

func (a *activity) Wait(ctx context.Context) tea.Msg {
	select {
	case <-a.m.Changed():
	case <-ctx.Done():
		return nil
	}
	select {
	case <-time.After(coalesce):
	case <-ctx.Done():
		return nil
	}
	events, seen := a.m.Events(a.seen)
	a.seen = seen
	var msg ChangedMsg
	var notices []string
	for _, e := range events {
		if e.Kind == jobs.EventFinished {
			msg.Finished = append(msg.Finished, e.Job)
		}
		if e.Kind == jobs.EventAdded && silenced(e.Job.ID) {
			continue
		}
		if n := Notice(e); n != "" {
			notices = append(notices, n)
		}
	}
	if len(notices) > 0 {
		// the latest, with how many more
		msg.notice = notices[len(notices)-1]
		if len(notices) > 1 {
			msg.notice += fmt.Sprintf(" (+%d)", len(notices)-1)
		}
	}
	return msg
}

// Notice is what the user is told of a job added or finished.
func Notice(e jobs.Event) string {
	j := e.Job
	if e.Kind == jobs.EventAdded {
		if j.Status == jobs.Queued {
			return fmt.Sprintf("Queued %s behind %s (%s: %d ahead)", j.Title, j.Behind, strings.Join(j.Providers, ", "), j.Ahead)
		}
		verb := "Downloading "
		if j.Kind == jobs.KindCheck {
			verb = "Checking "
		}
		return verb + j.Title + " (" + base.KeyDownloads.Help().Key + ": downloads)"
	}
	switch j.Status {
	case jobs.Cancelled:
		return "Cancelled " + j.Title
	case jobs.Failed:
		return j.Title + ": " + j.Err.Error()
	case jobs.Done:
		if j.Read {
			return "Ready to read: " + j.Title
		}
		if j.Kind == jobs.KindCheck {
			return capitalize(j.Message)
		}
		return "Downloaded " + j.Title + ": " + j.Message
	}
	return ""
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func (a *activity) Status() string {
	running, queued := 0, 0
	for _, j := range a.m.Active() {
		if j.Status == jobs.Queued {
			queued++
		} else {
			running++
		}
	}
	var parts []string
	if running > 0 {
		parts = append(parts, fmt.Sprintf("%d running", running))
	}
	if queued > 0 {
		parts = append(parts, fmt.Sprintf("%d queued", queued))
	}
	if len(parts) == 0 {
		return ""
	}
	return icon.Download.Raw() + " " + strings.Join(parts, " · ") + " (" + base.KeyDownloads.Help().Key + ")"
}

func (a *activity) Running() []string {
	var lines []string
	for _, j := range a.m.Active() {
		lines = append(lines, Line(j))
	}
	return lines
}

func (a *activity) Shutdown(timeout time.Duration) {
	if !a.m.Shutdown(timeout) {
		log.Warn("tui: background jobs still running after %s", timeout)
	}
}

// Line is the job in a line: "download One Piece: 3 chapters (queued)".
func Line(j jobs.Job) string {
	return fmt.Sprintf("%s %s (%s)", j.Kind, j.Title, j.Status)
}

// Mark is the icon of a job's status for the lists: queued, or downloading (waiting too).
func Mark(status jobs.Status) string {
	if status == jobs.Queued {
		return icon.Queued.Colored()
	}
	return icon.Progress.Colored()
}

// ChapterMark is the mark of a chapter in an active download, empty if it isn't.
func ChapterMark(provider, mangaID string, chapter mangadata.Chapter) string {
	if st, ok := Manager().ChapterStatus(provider, mangaID, chapter); ok {
		return Mark(st)
	}
	return ""
}

// MangaMark is the mark of a manga with an active download, empty without one.
func MangaMark(provider, mangaID string) string {
	if st, ok := Manager().MangaStatus(provider, mangaID); ok {
		return Mark(st)
	}
	return ""
}

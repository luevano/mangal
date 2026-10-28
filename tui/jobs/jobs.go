// Package jobs is the TUI's background downloads and checks: the process's core.Jobs (over
// one core/jobs.Manager), its line in the header, its notices, the quit question
// (base.Activity) and the marks of the lists.
package jobs

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/notify"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/tui/base"
)

var (
	mu      sync.Mutex
	manager *jobs.Manager
	// remote are a mangal serve's jobs (StartRemote), nil for the process's
	remote core.Jobs

	// quiet are the jobs whose screen told the user they were added, the activity skips
	// their added notice. quietMu is held across the Add, so the activity can't see the
	// event before the id is here.
	quietMu sync.Mutex
	quiet   = map[int64]bool{}
)

// Start makes the process's manager, its jobs run with ctx; it's the TUI's base.Activity.
func Start(ctx context.Context) base.Activity {
	m := jobs.New(ctx)
	SetManager(m)
	return &activity{svc: Service(), shutdown: m.Shutdown}
}

// StartRemote is Start on a mangal serve's jobs: the downloads are the server's (they go
// on when the TUI quits), the manager runs the rest (the watch checks it asks for).
func StartRemote(ctx context.Context, svc core.Jobs) base.Activity {
	m := jobs.New(ctx)
	mu.Lock()
	manager, remote = m, svc
	mu.Unlock()
	return &activity{svc: svc, shutdown: m.Shutdown, remote: true}
}

// Manager is the process's manager (one with a background ctx if Start wasn't called, for
// tests and the standalone screens): it runs the jobs Service doesn't take yet (watch
// checks, fills, the maintenance kinds).
func Manager() *jobs.Manager {
	mu.Lock()
	defer mu.Unlock()
	if manager == nil {
		manager = jobs.New(context.Background())
	}
	return manager
}

// Service is the process's jobs as the core's service: the screens go through it.
func Service() core.Jobs {
	mu.Lock()
	r := remote
	mu.Unlock()
	if r != nil {
		return r
	}
	return core.NewJobs(Manager(), core.JobsOptions{Finally: notifyDone})
}

// SetManager replaces the manager, for tests.
func SetManager(m *jobs.Manager) {
	mu.Lock()
	defer mu.Unlock()
	manager = m
}

// Download queues the download (req.Origin is the window key of the screen that starts
// it), silent if the screen tells the user itself (no "downloading" notice). A desktop
// notification goes when it ends (notification.tui).
func Download(ctx context.Context, req core.DownloadReq, silent bool) (core.Job, error) {
	if !silent {
		return Service().Download(ctx, req)
	}
	quietMu.Lock()
	defer quietMu.Unlock()
	job, err := Service().Download(ctx, req)
	if err == nil {
		quiet[job.ID] = true
	}
	return job, err
}

// Queue adds a download job made elsewhere (a fill's, core.FillPlan.Job), with Download's
// desktop notification when it ends.
func Queue(spec jobs.Spec) jobs.Job {
	spec.Finally = notifyDone
	return Manager().Add(spec)
}

// notifyDone sends the desktop notification of a download that ended (notification.tui),
// not for reading.
func notifyDone(ctx context.Context, j jobs.Job) {
	if j.Status == jobs.Cancelled || j.Read || !config.Notification.TUI.Get() {
		return
	}
	if err := notify.SendContext(ctx, j.Results); err != nil {
		log.Warn("send the notification: %s", err)
	}
}

// silenced is whether the job's added notice is skipped (once).
func silenced(id int64) bool {
	quietMu.Lock()
	defer quietMu.Unlock()
	ok := quiet[id]
	delete(quiet, id)
	return ok
}

// Check adds a check of the watches with the ids (of the providers) through m as a job.
func Check(m watch.Manager, ids []int64, providers []string, opts watch.CheckOptions, title, origin string, summary func(watch.Report) string) jobs.Job {
	return Manager().Add(jobs.CheckSpec(m, ids, providers, opts, title, origin, summary))
}

// ChangedMsg is jobs changing: Finished are the ones over since the last one. Every
// screen gets it while shown (base.Activity).
type ChangedMsg struct {
	Finished []core.Job
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
	svc      core.Jobs
	shutdown func(time.Duration) bool
	// remote: svc's jobs are a server's, quitting doesn't stop them
	remote bool

	subscribe sync.Once
	events    <-chan core.JobEvent
}

// coalesce is how long changes are gathered into one msg (a page progress is many).
const coalesce = 100 * time.Millisecond

func (a *activity) Wait(ctx context.Context) tea.Msg {
	a.subscribe.Do(func() {
		var err error
		if a.events, err = a.svc.Subscribe(ctx, 0); err != nil {
			log.Warn("tui: jobs' events: %s", err)
		}
	})
	if a.events == nil {
		return nil
	}
	var events []core.JobEvent
	select {
	case e, ok := <-a.events:
		if !ok {
			return nil
		}
		events = append(events, e)
	case <-ctx.Done():
		return nil
	}
	select {
	case <-time.After(coalesce):
	case <-ctx.Done():
		return nil
	}
more:
	for {
		select {
		case e, ok := <-a.events:
			if !ok {
				break more
			}
			events = append(events, e)
		default:
			break more
		}
	}
	var msg ChangedMsg
	var notices []string
	for _, e := range events {
		if e.Kind == core.JobFinished {
			msg.Finished = append(msg.Finished, e.Job)
		}
		if e.Kind == core.JobAdded && silenced(e.Job.ID) {
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
func Notice(e core.JobEvent) string {
	j := e.Job
	if e.Kind == core.JobChanged {
		return ""
	}
	if e.Kind == core.JobAdded {
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
		return j.Title + ": " + j.Error
	case jobs.Done:
		if j.Read {
			return "Ready to read: " + j.Title
		}
		switch j.Kind {
		case jobs.KindCheck:
			return capitalize(j.Message)
		case jobs.KindDownload:
			return "Downloaded " + j.Title + ": " + j.Message
		}
		// a remote server's scans, lookups
		return capitalize(j.Title) + ": " + j.Message
	}
	return ""
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// cached are jobs that keep their list (a remote server's): drawn on every view, no
// request each time.
type cached interface{ Active() []core.Job }

// active are the active jobs.
func (a *activity) active() []core.Job {
	if c, ok := a.svc.(cached); ok {
		return c.Active()
	}
	// the local jobs answer at once
	list, err := a.svc.List(context.Background())
	if err != nil {
		log.Warn("tui: list the jobs: %s", err)
	}
	return slices.DeleteFunc(list, func(j core.Job) bool { return !j.Status.Active() })
}

func (a *activity) Status() string {
	running, queued := 0, 0
	for _, j := range a.active() {
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
	list := a.active()
	if a.remote {
		list = nil
		for _, j := range Manager().Active() {
			list = append(list, core.JobOf(j))
		}
	}
	var lines []string
	for _, j := range list {
		lines = append(lines, Line(j))
	}
	return lines
}

func (a *activity) Shutdown(timeout time.Duration) {
	if !a.shutdown(timeout) {
		log.Warn("tui: background jobs still running after %s", timeout)
	}
}

// Line is the job in a line: "download One Piece: 3 chapters (queued)".
func Line(j core.Job) string {
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
	if st, ok := Service().ChapterStatus(provider, mangaID, core.ChapterRefOf(chapter)); ok {
		return Mark(st)
	}
	return ""
}

// MangaMark is the mark of a manga with an active download, empty without one.
func MangaMark(provider, mangaID string) string {
	if st, ok := Service().MangaStatus(provider, mangaID); ok {
		return Mark(st)
	}
	return ""
}

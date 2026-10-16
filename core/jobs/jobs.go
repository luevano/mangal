// Package jobs runs downloads and watch checks in the background, one at a time per
// provider: a job of a busy provider waits in that provider's queue (FIFO) instead of being
// refused. The TUI has one Manager (tui/jobs).
package jobs

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client"
)

// Status is where a job is.
type Status int

const (
	// Queued is waiting for its turn: another job of this process has its provider.
	Queued Status = iota
	// Waiting is its turn, but another mangal (serve, the CLI) holds the provider's lock.
	Waiting
	// Running is doing its work.
	Running
	// Done is over without errors.
	Done
	// Failed is over with an error (a download: some chapter failed).
	Failed
	// Cancelled was stopped (or left the queue) before it was over.
	Cancelled
)

func (s Status) String() string {
	switch s {
	case Queued:
		return "queued"
	case Waiting:
		return "waiting"
	case Running:
		return "running"
	case Done:
		return "done"
	case Failed:
		return "failed"
	case Cancelled:
		return "cancelled"
	}
	return fmt.Sprintf("status %d", int(s))
}

// Active is true until the job is over.
func (s Status) Active() bool { return s < Done }

// Kind is what a job does.
type Kind int

const (
	KindDownload Kind = iota
	KindCheck
	// KindScan is a library scan (mangal serve's).
	KindScan
)

func (k Kind) String() string {
	switch k {
	case KindCheck:
		return "check"
	case KindScan:
		return "scan"
	}
	return "download"
}

// keepFinished is how many finished jobs are kept listed.
const keepFinished = 50

// keepEvents is how many events are kept for Events.
const keepEvents = 200

// Job is a job as it was when asked for (a copy).
type Job struct {
	ID    int64
	Kind  Kind
	Title string
	// Providers are the ones it downloads from: it waits in each one's queue.
	Providers []string
	// MangaID is the provider's manga of a download.
	MangaID string
	// Origin is the window key of the screen that started it.
	Origin string
	// Read is a download for reading.
	Read   bool
	Status Status
	// Message is what it does now (or did last, once over).
	Message string
	// Note is the provider's latest status (a mirror switch, a rate limit), NoteAt when.
	Note   string
	NoteAt time.Time
	// Done of Total steps (a download's chapters), Part the share of the current one.
	Done, Total int
	Part        float64
	Err         error
	// Results are a download's chapters (copies).
	Results                  client.Results
	Added, Started, Finished time.Time
	// Ahead is how many jobs go before it in its providers' queues while Queued, Behind
	// the title of the first of them.
	Ahead  int
	Behind string
}

// Spec is a job to add.
type Spec struct {
	Kind      Kind
	Title     string
	Providers []string
	MangaID   string
	Origin    string
	Read      bool
	// Chapters are a download's, Run gets the ones to download from Progress.Batch.
	Chapters []mangadata.Chapter
	// Run does the work, reporting through p. A cancelled ctx stops it.
	Run func(ctx context.Context, p *Progress) error
	// Finally runs after Run (in the background, e.g. a desktop notification) with the
	// job as it ended, may be nil. ctx is the manager's.
	Finally func(ctx context.Context, j Job)
}

// EventKind is what an Event is about.
type EventKind int

const (
	// EventAdded is a job added: Job.Status says if it started or is queued.
	EventAdded EventKind = iota
	// EventFinished is a job over (done, failed or cancelled).
	EventFinished
)

// Event is a job added or finished, see Manager.Events.
type Event struct {
	Seq  uint64
	Kind EventKind
	Job  Job
}

type entry struct {
	Job
	results  client.Results
	chapters map[string]bool
	run      func(context.Context, *Progress) error
	finally  func(context.Context, Job)
	cancel   context.CancelFunc
	// seq is its place in the queues, a retry queues it again at the end
	seq int64
	// held are its providers while it runs, until it releases them
	held map[string]bool
}

// Manager runs the jobs. Every method is safe to call from any goroutine.
type Manager struct {
	mu      sync.Mutex
	ctx     context.Context
	stop    context.CancelFunc
	wg      sync.WaitGroup
	ids     int64
	seq     int64
	entries []*entry
	// held is the job running each provider
	held    map[string]*entry
	changed chan struct{}
	events  []Event
	evSeq   uint64
	closed  bool
	now     func() time.Time
}

// New is a manager whose jobs run with ctx (cancelled: they stop).
func New(ctx context.Context) *Manager {
	m := &Manager{held: map[string]*entry{}, changed: make(chan struct{}), now: time.Now}
	m.ctx, m.stop = context.WithCancel(ctx)
	return m
}

// ChapterKey is the chapter in a manga's jobs: its url, else its number and title.
func ChapterKey(c mangadata.Chapter) string {
	info := c.Info()
	if info.URL != "" {
		return info.URL
	}
	return fmt.Sprintf("%g\x00%s", info.Number, info.Title)
}

// resolveProvider is a provider's current id (client.CurrentProviderID), swapped in tests.
var resolveProvider = client.CurrentProviderID

// currentIDs are the providers' current ids, so an old id's job queues behind the
// current one's.
func currentIDs(providers []string) []string {
	ids := make([]string, len(providers))
	for i, p := range providers {
		ids[i] = resolveProvider(p)
	}
	return slices.Compact(slices.Sorted(slices.Values(ids)))
}

// Add queues the job, it starts at once if its providers are free. Its providers are
// known by their current ids (an old id is resolved).
func (m *Manager) Add(spec Spec) Job {
	providers := currentIDs(spec.Providers)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ids++
	m.seq++
	e := &entry{
		Job: Job{
			ID:        m.ids,
			Kind:      spec.Kind,
			Title:     spec.Title,
			Providers: providers,
			MangaID:   spec.MangaID,
			Origin:    spec.Origin,
			Read:      spec.Read,
			Status:    Queued,
			Added:     m.now(),
		},
		results: client.NewResults(spec.Chapters),
		run:     spec.Run,
		finally: spec.Finally,
		seq:     m.seq,
	}
	e.Total = len(e.results)
	e.indexChapters()
	m.entries = append(m.entries, e)
	if m.closed {
		e.Status, e.Finished, e.Err = Cancelled, m.now(), context.Canceled
		m.changedLocked()
		return m.snapshotLocked(e)
	}
	m.scheduleLocked()
	job := m.snapshotLocked(e)
	m.eventLocked(EventAdded, job)
	m.changedLocked()
	return job
}

func (e *entry) indexChapters() {
	e.chapters = map[string]bool{}
	for _, r := range e.results {
		if r.ToDownload() {
			e.chapters[ChapterKey(r.Chapter)] = true
		}
	}
}

// queuedLocked are the queued jobs in queue order.
func (m *Manager) queuedLocked() []*entry {
	var queued []*entry
	for _, e := range m.entries {
		if e.Status == Queued {
			queued = append(queued, e)
		}
	}
	slices.SortFunc(queued, func(a, b *entry) int { return int(a.seq - b.seq) })
	return queued
}

// scheduleLocked starts the queued jobs whose turn it is: first in the queue of each of
// their providers, none of them held. The oldest queued job is always first in its queues.
func (m *Manager) scheduleLocked() {
	if m.closed {
		return
	}
	claimed := map[string]bool{}
	for _, e := range m.queuedLocked() {
		free := true
		for _, p := range e.Providers {
			if m.held[p] != nil || claimed[p] {
				free = false
			}
		}
		if free {
			m.startLocked(e)
		}
		for _, p := range e.Providers {
			claimed[p] = true
		}
	}
}

func (m *Manager) startLocked(e *entry) {
	ctx, cancel := context.WithCancel(m.ctx)
	e.cancel = cancel
	e.Status, e.Started, e.Message = Running, m.now(), ""
	e.held = map[string]bool{}
	for _, p := range e.Providers {
		m.held[p] = e
		e.held[p] = true
	}
	m.wg.Add(1)
	go m.runJob(ctx, e)
}

func (m *Manager) runJob(ctx context.Context, e *entry) {
	defer m.wg.Done()
	var err error
	if e.run != nil {
		err = e.run(ctx, &Progress{m: m, e: e})
	}
	m.mu.Lock()
	e.cancel = nil
	m.releaseLocked(e, "")
	switch {
	case ctx.Err() != nil && (err == nil || errors.Is(err, context.Canceled)):
		e.Status, e.Err = Cancelled, nil
	case err != nil:
		e.Status, e.Err = Failed, err
	default:
		e.Status = Done
	}
	e.Finished = m.now()
	e.Part = 0
	job := m.snapshotLocked(e)
	m.eventLocked(EventFinished, job)
	m.trimLocked()
	m.scheduleLocked()
	m.changedLocked()
	finally := e.finally
	m.mu.Unlock()
	if finally != nil {
		finally(m.ctx, job)
	}
}

// releaseLocked frees the job's provider ("" every one) for the next job.
func (m *Manager) releaseLocked(e *entry, provider string) {
	for p := range e.held {
		if provider != "" && p != provider {
			continue
		}
		if m.held[p] == e {
			delete(m.held, p)
		}
		delete(e.held, p)
	}
}

// trimLocked drops the oldest finished jobs over keepFinished.
func (m *Manager) trimLocked() {
	finished := 0
	for _, e := range m.entries {
		if !e.Status.Active() {
			finished++
		}
	}
	for i := 0; finished > keepFinished && i < len(m.entries); {
		if !m.entries[i].Status.Active() {
			m.entries = slices.Delete(m.entries, i, i+1)
			finished--
			continue
		}
		i++
	}
}

func (m *Manager) eventLocked(kind EventKind, job Job) {
	m.evSeq++
	m.events = append(m.events, Event{Seq: m.evSeq, Kind: kind, Job: job})
	if len(m.events) > keepEvents {
		m.events = slices.Delete(m.events, 0, len(m.events)-keepEvents)
	}
}

func (m *Manager) changedLocked() {
	close(m.changed)
	m.changed = make(chan struct{})
}

// Changed is closed on the next change of any job.
func (m *Manager) Changed() <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.changed
}

// Events are the jobs added and finished after the event after (0: every kept one), and
// the last event's Seq to ask from next time.
func (m *Manager) Events(after uint64) ([]Event, uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var events []Event
	for _, e := range m.events {
		if e.Seq > after {
			events = append(events, e)
		}
	}
	return events, m.evSeq
}

func (m *Manager) snapshotLocked(e *entry) Job {
	job := e.Job
	job.Providers = slices.Clone(e.Providers)
	if len(e.results) != 0 {
		job.Results = make(client.Results, len(e.results))
		for i, r := range e.results {
			c := *r
			job.Results[i] = &c
		}
	}
	if e.Status == Queued {
		for _, o := range m.entries {
			if o == e || !o.Status.Active() || (o.Status == Queued && o.seq > e.seq) {
				continue
			}
			if slices.ContainsFunc(o.Providers, func(p string) bool { return slices.Contains(e.Providers, p) }) {
				if job.Ahead == 0 || o.Status != Queued {
					job.Behind = o.Title
				}
				job.Ahead++
			}
		}
	}
	return job
}

// Get is the job with the id, false if it isn't listed (anymore).
func (m *Manager) Get(id int64) (Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.findLocked(id); e != nil {
		return m.snapshotLocked(e), true
	}
	return Job{}, false
}

func (m *Manager) findLocked(id int64) *entry {
	for _, e := range m.entries {
		if e.ID == id {
			return e
		}
	}
	return nil
}

// Jobs are every listed job, in the order they were added.
func (m *Manager) Jobs() []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	jobs := make([]Job, len(m.entries))
	for i, e := range m.entries {
		jobs[i] = m.snapshotLocked(e)
	}
	return jobs
}

// Active are the jobs not over yet, in the order they were added.
func (m *Manager) Active() []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	var jobs []Job
	for _, e := range m.entries {
		if e.Status.Active() {
			jobs = append(jobs, m.snapshotLocked(e))
		}
	}
	return jobs
}

// Cancel stops the job: a queued one leaves its queue at once, a running one stops (its
// ctx is cancelled). False if it's over or not listed.
func (m *Manager) Cancel(id int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.findLocked(id)
	if e == nil || !e.Status.Active() {
		return false
	}
	m.cancelLocked(e)
	m.scheduleLocked()
	m.changedLocked()
	return true
}

func (m *Manager) cancelLocked(e *entry) {
	if e.Status == Queued {
		e.Status, e.Finished = Cancelled, m.now()
		m.eventLocked(EventFinished, m.snapshotLocked(e))
		m.trimLocked()
		return
	}
	if e.cancel != nil {
		e.cancel()
	}
}

// ClearQueue cancels the queued jobs of the provider ("" every provider), not the running
// ones; it's how many.
func (m *Manager) ClearQueue(provider string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, e := range m.queuedLocked() {
		if provider == "" || slices.Contains(e.Providers, provider) {
			m.cancelLocked(e)
			n++
		}
	}
	if n > 0 {
		m.changedLocked()
	}
	return n
}

// CancelAll cancels every queued and running job, it's how many.
func (m *Manager) CancelAll() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	// the queued ones first, so none starts when a running one stops
	for _, e := range m.queuedLocked() {
		m.cancelLocked(e)
		n++
	}
	for _, e := range m.entries {
		if e.Status.Active() {
			m.cancelLocked(e)
			n++
		}
	}
	if n > 0 {
		m.changedLocked()
	}
	return n
}

// ErrNothingToRetry is a retry of a job without failed (or left) chapters.
var ErrNothingToRetry = errors.New("nothing to retry")

// Retry queues the download again with its failed chapters (and the ones a cancel left).
func (m *Manager) Retry(id int64) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.findLocked(id)
	switch {
	case e == nil:
		return Job{}, fmt.Errorf("job %d isn't listed", id)
	case e.Status.Active():
		return Job{}, fmt.Errorf("job %d is %s", id, e.Status)
	case e.Kind != KindDownload:
		return Job{}, fmt.Errorf("retry %s: %w", e.Title, ErrNothingToRetry)
	}
	left := 0
	for _, r := range e.results {
		if r.Failed() {
			r.Err, r.Down = nil, nil
		}
		if r.ToDownload() {
			left++
		}
	}
	if left == 0 {
		return Job{}, fmt.Errorf("retry %s: %w", e.Title, ErrNothingToRetry)
	}
	m.seq++
	e.seq = m.seq
	e.Status, e.Err, e.Message, e.Done, e.Total, e.Part = Queued, nil, "", 0, left, 0
	e.Finished = time.Time{}
	e.indexChapters()
	m.scheduleLocked()
	job := m.snapshotLocked(e)
	m.eventLocked(EventAdded, job)
	m.changedLocked()
	return job, nil
}

// Remove drops a finished job from the list, false if it's active or not listed.
func (m *Manager) Remove(id int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, e := range m.entries {
		if e.ID == id && !e.Status.Active() {
			m.entries = slices.Delete(m.entries, i, i+1)
			m.changedLocked()
			return true
		}
	}
	return false
}

// MangaStatus is the most advanced status of the active downloads of the provider's
// manga (running over waiting over queued), false without any.
func (m *Manager) MangaStatus(provider, mangaID string) (Status, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	status, ok := Queued, false
	for _, e := range m.entries {
		if e.Kind == KindDownload && e.Status.Active() && e.MangaID == mangaID && slices.Contains(e.Providers, provider) {
			status, ok = max(status, e.Status), true
		}
	}
	return status, ok
}

// ChapterStatus is the status of the active download that has the chapter of the
// provider's manga yet to download, false without one.
func (m *Manager) ChapterStatus(provider, mangaID string, chapter mangadata.Chapter) (Status, bool) {
	key := ChapterKey(chapter)
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.entries {
		if e.Kind == KindDownload && e.Status.Active() && e.MangaID == mangaID && e.chapters[key] && slices.Contains(e.Providers, provider) {
			return e.Status, true
		}
	}
	return Queued, false
}

// Shutdown cancels every job and waits for them up to timeout, false if some didn't stop
// in time. No job starts after it.
func (m *Manager) Shutdown(timeout time.Duration) bool {
	m.mu.Lock()
	m.closed = true
	for _, e := range m.queuedLocked() {
		m.cancelLocked(e)
	}
	m.changedLocked()
	m.mu.Unlock()
	m.stop()
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// Progress is how a job's Run reports.
type Progress struct {
	m *Manager
	e *entry
}

func (p *Progress) update(f func(e *entry)) {
	p.m.mu.Lock()
	defer p.m.mu.Unlock()
	f(p.e)
	p.m.changedLocked()
}

// Waiting is the job waiting for another mangal to free a provider, message says which.
func (p *Progress) Waiting(message string) {
	p.update(func(e *entry) { e.Status, e.Message = Waiting, message })
}

// Running is the job at work (again, after Waiting).
func (p *Progress) Running(message string) {
	p.update(func(e *entry) { e.Status, e.Message = Running, message })
}

// Message is what the job does now.
func (p *Progress) Message(message string) {
	p.update(func(e *entry) { e.Message = message })
}

// Note is the provider's latest status, shown a while next to the message.
func (p *Progress) Note(note string) {
	p.update(func(e *entry) { e.Note, e.NoteAt = note, p.m.now() })
}

// Step is how many of total steps are done.
func (p *Progress) Step(done, total int) {
	p.update(func(e *entry) { e.Done, e.Total, e.Part = done, total, 0 })
}

// Part is the share of the current step that's done.
func (p *Progress) Part(part float64) {
	p.update(func(e *entry) { e.Part = part })
}

// Release frees the provider for the next job before this one is over (a check done
// with it).
func (p *Progress) Release(provider string) {
	p.m.mu.Lock()
	defer p.m.mu.Unlock()
	if !p.e.held[provider] {
		return
	}
	p.m.releaseLocked(p.e, provider)
	p.m.scheduleLocked()
	p.m.changedLocked()
}

// Batch are a download's chapters to download now (all, or what a retry left).
func (p *Progress) Batch() client.Results {
	p.m.mu.Lock()
	defer p.m.mu.Unlock()
	return p.e.results.ToDownload()
}

// Result sets the result of a chapter of the batch (one of Batch's).
func (p *Progress) Result(r *client.Result, result client.Result) {
	p.update(func(e *entry) {
		*r = result
		if !r.ToDownload() {
			delete(e.chapters, ChapterKey(r.Chapter))
		}
	})
}

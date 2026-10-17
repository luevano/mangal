package watch

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/storage"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// Kinds of LiveEvent.
const (
	// LiveRunStart is a check run that started (Message says what it checks).
	LiveRunStart = "run_start"
	// LiveProviderStart is a provider whose watches start being checked.
	LiveProviderStart = "provider_start"
	// LiveStart is a watch being checked.
	LiveStart = "start"
	// LiveFound is a watch's chapters listed: how many, which ones to download.
	LiveFound = "found"
	// LiveChapter is a chapter being downloaded (Chapter is set).
	LiveChapter = "chapter"
	// LiveWait is a rate limit waited out before trying the chapter again.
	LiveWait = "wait"
	// LiveProgress is a routine step: the page progress of the chapter being downloaded
	// (Chapter, Pages and PagesDone are set), a page of results. Not kept for new subscribers.
	LiveProgress = "progress"
	// LiveStatus is what the provider is doing (e.g. a mirror down, a rate limit).
	LiveStatus = "status"
	// LiveChapterDone is a chapter downloaded, failed or unavailable.
	LiveChapterDone = "chapter_done"
	// LiveDone is a watch checked (Status is its result's).
	LiveDone = "done"
	// LiveProviderDone is a provider whose watches are done.
	LiveProviderDone = "provider_done"
	// LiveRunDone is a check run that finished.
	LiveRunDone = "run_done"
	// LiveQueued is a scheduled check (mangal serve) waiting for a provider someone else
	// has (Message says who), it runs once the provider is free. No run yet.
	LiveQueued = "queued"
	// LiveHealthStart, LiveHealthStatus and LiveHealthDone are a health job of mangal serve
	// (Job is set): started, what it's doing, finished (Message has the counts or the
	// error).
	LiveHealthStart  = "health_start"
	LiveHealthStatus = "health_status"
	LiveHealthDone   = "health_done"
)

// LiveEvent is the progress of a check as it happens, see Manager.Subscribe. It's what
// `GET /api/events` streams.
type LiveEvent struct {
	// Seq orders the events of one process (server), from 1.
	Seq  uint64    `json:"seq"`
	Time time.Time `json:"time"`
	// Run is the check run, Job the health job (GET /api/health?network=1) instead.
	Run      int64    `json:"run"`
	Job      int64    `json:"job,omitempty"`
	Kind     string   `json:"kind"`
	Provider string   `json:"provider,omitempty"`
	Watch    int64    `json:"watch,omitempty"`
	Title    string   `json:"title,omitempty"`
	Chapter  *float32 `json:"chapter,omitempty"`
	// Message is the event in words, e.g. "downloading chapter 65".
	Message string `json:"message"`
	// Status is the watch's result on LiveDone (new, none, error, skipped).
	Status string `json:"status,omitempty"`
	// Pages is the chapter's page count (LiveProgress, LiveChapterDone), PagesDone how
	// many are downloaded (LiveProgress).
	Pages     int `json:"pages,omitempty"`
	PagesDone int `json:"pages_done,omitempty"`
}

// ProviderStatus is a provider being checked.
type ProviderStatus struct {
	Provider string `json:"provider"`
	// Since is when its check took it (its lock).
	Since time.Time `json:"since"`
	// PID and Host hold its lock.
	PID  int    `json:"pid,omitempty"`
	Host string `json:"host,omitempty"`
	// The rest is known only for the checks of the process answering (the server's, or
	// the local service's own): the check run, the watch being checked and since when,
	// the chapter being downloaded and the state in words.
	Run        int64     `json:"run,omitempty"`
	Watch      int64     `json:"watch,omitempty"`
	Title      string    `json:"title,omitempty"`
	WatchSince time.Time `json:"watch_since,omitzero"`
	Chapter    *float32  `json:"chapter,omitempty"`
	State      string    `json:"state,omitempty"`
}

const (
	// recentPerRun is how many events of a check run Hub keeps for new subscribers.
	recentPerRun = 300
	// keepFinished is how many finished runs Hub keeps the events of.
	keepFinished = 3
	// subscriberBuffer is how many events a subscriber can be behind before it's dropped.
	subscriberBuffer = 512
)

// Hub broadcasts the LiveEvents of the checks of a process and keeps the recent ones (of
// the running checks and the last few finished), so a subscriber that comes mid-check
// sees what happened. It also keeps what each provider being checked is doing.
type Hub struct {
	mu       sync.Mutex
	seq      uint64
	subs     map[chan LiveEvent]struct{}
	recent   map[int64][]LiveEvent
	finished []int64
	active   map[string]*ProviderStatus
}

// NewHub is an empty hub.
func NewHub() *Hub {
	return &Hub{subs: map[chan LiveEvent]struct{}{}, recent: map[int64][]LiveEvent{}, active: map[string]*ProviderStatus{}}
}

// Subscribe is the events from now on, after the recent ones; closed once ctx is done or
// when the subscriber falls too far behind (subscribe again, the recent events come
// again with their Seq).
func (h *Hub) Subscribe(ctx context.Context) <-chan LiveEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	recent := h.recentLocked()
	ch := make(chan LiveEvent, subscriberBuffer+len(recent))
	for _, e := range recent {
		ch <- e
	}
	h.subs[ch] = struct{}{}
	go func() {
		<-ctx.Done()
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
	}()
	return ch
}

// Seq is the Seq of the last event.
func (h *Hub) Seq() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.seq
}

// Recent are the kept events, by Seq.
func (h *Hub) Recent() []LiveEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.recentLocked()
}

func (h *Hub) recentLocked() []LiveEvent {
	var all []LiveEvent
	for _, events := range h.recent {
		all = append(all, events...)
	}
	slices.SortFunc(all, func(a, b LiveEvent) int { return cmp.Compare(a.Seq, b.Seq) })
	return all
}

// Active is what the providers being checked by this process do, by provider.
func (h *Hub) Active() []ProviderStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	list := make([]ProviderStatus, 0, len(h.active))
	for _, ps := range h.active {
		list = append(list, *ps)
	}
	slices.SortFunc(list, func(a, b ProviderStatus) int { return strings.Compare(a.Provider, b.Provider) })
	return list
}

// begin marks the provider taken by the run (queued until its watches start).
func (h *Hub) begin(provider string, run int64, since time.Time) {
	host, _ := os.Hostname()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.active[provider] = &ProviderStatus{Provider: provider, Since: since, PID: os.Getpid(), Host: host, Run: run, State: "queued"}
}

// end marks the provider free.
func (h *Hub) end(provider string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.active, provider)
}

// Publish sends an event that isn't a check's (a health job's) to the subscribers.
func (h *Hub) Publish(e LiveEvent) { h.publish(e) }

// publish sends the event to the subscribers.
func (h *Hub) publish(e LiveEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	e.Seq = h.seq
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	h.track(e)
	// a job's events are kept apart from the runs'
	key := e.Run
	if e.Job != 0 {
		key = -e.Job
	}
	// one per page would push the rest out
	if e.Kind != LiveProgress {
		events := append(h.recent[key], e)
		if len(events) > recentPerRun {
			events = slices.Delete(events, 0, len(events)-recentPerRun)
		}
		h.recent[key] = events
	}
	if e.Kind == LiveRunDone || e.Kind == LiveHealthDone {
		h.finished = append(h.finished, key)
		if len(h.finished) > keepFinished {
			delete(h.recent, h.finished[0])
			h.finished = h.finished[1:]
		}
	}
	for ch := range h.subs {
		select {
		case ch <- e:
		default:
			// too far behind, it'll subscribe again
			delete(h.subs, ch)
			close(ch)
		}
	}
}

// track keeps the provider's state.
func (h *Hub) track(e LiveEvent) {
	ps, ok := h.active[e.Provider]
	if !ok {
		return
	}
	switch e.Kind {
	case LiveProviderStart:
		ps.State = "checking"
	case LiveStart:
		ps.Watch, ps.Title, ps.WatchSince, ps.Chapter, ps.State = e.Watch, e.Title, e.Time, nil, "checking"
	case LiveFound, LiveChapter, LiveWait, LiveProgress, LiveStatus:
		ps.State = e.Message
		if e.Chapter != nil {
			ps.Chapter = e.Chapter
		}
	case LiveDone:
		ps.Watch, ps.Title, ps.WatchSince, ps.Chapter, ps.State = 0, "", time.Time{}, nil, "checking"
	}
}

// Subscribe is the live events of this process's checks (the TUI in local mode sees its
// own checks; the ones of another process show only in Status).
func (s *Service) Subscribe(ctx context.Context) (<-chan LiveEvent, error) {
	if s.Events == nil {
		return nil, errors.New("no live events here")
	}
	return s.Events.Subscribe(ctx), nil
}

// Live is the LiveEvent of the event (without its run), ok false for none.
func (e Event) Live() (LiveEvent, bool) {
	return liveEvent(0, e)
}

// Line is the event as a log line, with its manga (and provider when it's the found
// chapters) first, e.g. "One Piece (mangadex): found 1124 chapters, 2 to download (1123, 1124)".
func (e LiveEvent) Line() string {
	switch {
	case e.Title == "":
		return e.Message
	case e.Kind == LiveFound && e.Provider != "":
		return fmt.Sprintf("%s (%s): %s", e.Title, e.Provider, e.Message)
	}
	return e.Title + ": " + e.Message
}

// liveEvent is the LiveEvent of a check's Event, ok false for none.
func liveEvent(run int64, e Event) (LiveEvent, bool) {
	le := LiveEvent{Run: run, Provider: e.Watch.Provider, Watch: e.Watch.ID, Title: e.Watch.Title}
	switch e.Kind {
	case EventStart:
		le.Kind, le.Message = LiveStart, "checking "+e.Watch.Title
	case EventFound:
		le.Kind, le.Message = LiveFound, describeFound(*e.Found)
	case EventChapter:
		c := e.Chapter
		n := c.Result.Chapter.Info().Number
		le.Chapter = &n
		num := stringutil.FormatFloat32(n)
		switch c.Kind {
		case client.EventStart:
			le.Kind, le.Message = LiveChapter, "downloading chapter "+num
		case client.EventWait:
			le.Kind, le.Message = LiveWait, fmt.Sprintf("chapter %s rate limited (retry #%d), waiting %s", num, c.Attempt, c.Wait)
		case client.EventProgress:
			le.Kind, le.Message = LiveProgress, fmt.Sprintf("chapter %s: %s", num, c)
			if c.Progress.Total > 0 {
				le.Pages, le.PagesDone = c.Progress.Total, c.Progress.Done
			}
		case client.EventDone:
			le.Kind = LiveChapterDone
			r := c.Result
			switch {
			case r.Err != nil:
				le.Message = fmt.Sprintf("chapter %s failed: %s", num, r.Err)
			case r.Unavailable != "":
				le.Message = fmt.Sprintf("chapter %s unavailable: %s", num, r.Unavailable)
			case r.Down != nil && r.Down.Skipped != "":
				le.Message = fmt.Sprintf("chapter %s already there (%s)", num, r.Down.Skipped)
			default:
				le.Message = "downloaded chapter " + num
				if r.Down != nil && r.Down.Pages > 0 {
					le.Pages = r.Down.Pages
					le.Message += fmt.Sprintf(" (%d pages)", r.Down.Pages)
				}
			}
		default:
			return le, false
		}
	case EventBusy:
		le.Kind, le.Message = LiveWait, e.Busy.Message()
	case EventStatus:
		le.Kind, le.Message = LiveStatus, e.Status.String()
		if e.Status.IsMinor() {
			le.Kind = LiveProgress
		}
	case EventDone:
		le.Kind, le.Message, le.Status = LiveDone, outcome(*e.Result, false), e.Result.Status
		if e.Result.Title != "" {
			le.Title = e.Result.Title
		}
	default:
		return le, false
	}
	return le, true
}

// Found is what a check found of a watch, before downloading.
type Found struct {
	// Chapters is how many the provider lists.
	Chapters int `json:"chapters"`
	// Picked are the numbers of the ones to download.
	Picked []float32 `json:"picked,omitempty"`
	// From is the last downloaded chapter, nil when none.
	From        *float32 `json:"from,omitempty"`
	Unavailable int      `json:"unavailable,omitempty"`
	// Skipped are the ones the watch's skip list left out.
	Skipped []float32 `json:"skipped,omitempty"`
	// Coverage and HeldBack are Result's.
	Coverage string    `json:"coverage,omitempty"`
	HeldBack []float32 `json:"held_back,omitempty"`
}

func describeFound(f Found) string {
	s := "found " + stringutil.Quantify(f.Chapters, "chapter", "chapters")
	from := ""
	if f.From != nil {
		from = " from " + stringutil.FormatFloat32(*f.From)
	}
	switch n := len(f.Picked); {
	case n == 0:
		s += ", nothing new" + from
	case n <= 5:
		numbers := make([]string, n)
		for i, p := range f.Picked {
			numbers[i] = stringutil.FormatFloat32(p)
		}
		s += fmt.Sprintf(", %d to download (%s)", n, strings.Join(numbers, ", "))
	default:
		s += fmt.Sprintf(", %d to download (%s → %s)", n, stringutil.FormatFloat32(f.Picked[0]), stringutil.FormatFloat32(f.Picked[n-1]))
	}
	if f.Unavailable > 0 {
		s += fmt.Sprintf(", %d unavailable", f.Unavailable)
	}
	if len(f.Skipped) > 0 {
		s += ", skipped " + FormatNumbers(f.Skipped)
	}
	if f.Coverage != "" {
		s += "; " + f.Coverage + heldBack(f.HeldBack)
	}
	return s
}

// heldBack is ", held back 60, 61 (a gap before them)" for the chapters a check left for
// another provider, "" for none.
func heldBack(ns []float32) string {
	if len(ns) == 0 {
		return ""
	}
	return ", held back " + FormatNumbers(ns) + " (a gap before them)"
}

// runStartMessage says what a run checks.
func runStartMessage(run storage.CheckRun, providers []string, watches int, busy []string) string {
	s := fmt.Sprintf("%s check %d: %s of %s", run.Kind, run.ID, stringutil.Quantify(watches, "watch", "watches"), strings.Join(providers, ", "))
	if len(busy) > 0 {
		s += " (busy, skipped: " + strings.Join(busy, ", ") + ")"
	}
	return s
}

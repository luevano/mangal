package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// The topics of /api/v1/events.
const (
	TopicWatch  = "watch"
	TopicJob    = "job"
	TopicOp     = "op"
	TopicHealth = "health"
	TopicLog    = "log"
)

// Topics are every topic.
var Topics = []string{TopicWatch, TopicJob, TopicOp, TopicHealth, TopicLog}

// Event is an event of /api/v1/events.
type Event struct {
	Seq     uint64    `json:"seq"`
	Time    time.Time `json:"time"`
	Topic   string    `json:"topic"`
	Kind    string    `json:"kind"`
	Message string    `json:"message,omitempty"`
	// Data is the topic's object: a watch.LiveEvent, a Job, a core.OpResult, a LogEntry.
	Data any `json:"data,omitempty"`
}

const (
	// keepEvents is how many events are kept for new subscribers (Last-Event-ID).
	keepEvents = 500
	// eventsBuffer is how many events a subscriber can be behind before it's dropped.
	eventsBuffer = 512
	// jobChangesEvery is how often the running jobs' progress is sent.
	jobChangesEvery = time.Second
)

// Events is the event hub of the server: one seq over every topic, the recent events kept.
type Events struct {
	mu     sync.Mutex
	seq    uint64
	recent []Event
	subs   map[chan Event]map[string]bool
	now    func() time.Time
}

// NewEvents is an empty hub.
func NewEvents() *Events {
	return &Events{subs: map[chan Event]map[string]bool{}, now: time.Now}
}

// Publish sends an event to the subscribers of its topic.
func (h *Events) Publish(topic, kind, message string, data any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	e := Event{Seq: h.seq, Time: h.now(), Topic: topic, Kind: kind, Message: message, Data: data}
	// the page progress of the downloads is only for the live subscribers
	if !(topic == TopicWatch && kind == watch.LiveProgress) && !(topic == TopicJob && kind == "changed") {
		h.recent = append(h.recent, e)
		if len(h.recent) > keepEvents {
			h.recent = slices.Delete(h.recent, 0, len(h.recent)-keepEvents)
		}
	}
	for ch, topics := range h.subs {
		if !topics[topic] {
			continue
		}
		select {
		case ch <- e:
		default:
			// too far behind: it reconnects from its last event
			delete(h.subs, ch)
			close(ch)
		}
	}
}

// Seq is the seq of the last event.
func (h *Events) Seq() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.seq
}

// Subscribe is the events of the topics after since (the recent ones kept), then as they
// come; closed once ctx is done or when the subscriber falls too far behind.
func (h *Events) Subscribe(ctx context.Context, topics []string, since uint64) <-chan Event {
	want := map[string]bool{}
	for _, t := range topics {
		want[t] = true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if since > h.seq {
		// the server restarted since: everything is new
		since = 0
	}
	var recent []Event
	for _, e := range h.recent {
		if e.Seq > since && want[e.Topic] {
			recent = append(recent, e)
		}
	}
	ch := make(chan Event, eventsBuffer+len(recent))
	for _, e := range recent {
		ch <- e
	}
	h.subs[ch] = want
	context.AfterFunc(ctx, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
	})
	return ch
}

// forwardWatches publishes the watch manager's live events (the health jobs' as health)
// until ctx is done.
func (h *Events) forwardWatches(ctx context.Context, m watch.Manager) {
	var last uint64
	for ctx.Err() == nil {
		events, err := m.Subscribe(ctx)
		if err != nil {
			log.Warn("events: %s", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		for e := range events {
			if e.Seq <= last {
				continue
			}
			last = e.Seq
			topic := TopicWatch
			if e.Job != 0 || strings.HasPrefix(e.Kind, "health_") {
				topic = TopicHealth
			}
			h.Publish(topic, e.Kind, e.Message, e)
		}
	}
}

// forwardJobs publishes the jobs added and finished, and the running ones' progress (at
// most every second), until ctx is done.
func (h *Events) forwardJobs(ctx context.Context, m *jobs.Manager) {
	var last uint64
	var lastChange time.Time
	for {
		changed := m.Changed()
		events, seq := m.Events(last)
		last = seq
		for _, e := range events {
			kind := "added"
			if e.Kind == jobs.EventFinished {
				kind = "finished"
			}
			job := toAPIJob(core.JobOf(e.Job))
			h.Publish(TopicJob, kind, fmt.Sprintf("%s %s: %s", job.Kind, kind, job.Title), job)
		}
		if time.Since(lastChange) >= jobChangesEvery {
			lastChange = time.Now()
			for _, j := range m.Active() {
				if j.Status == jobs.Running {
					job := toAPIJob(core.JobOf(j))
					h.Publish(TopicJob, "changed", deref(job.Message), job)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-changed:
		case <-time.After(jobChangesEvery):
		}
	}
}

// forwardLogs publishes the lines logged to the log files (by any mangal) until ctx is
// done.
func (h *Events) forwardLogs(ctx context.Context, dir string) {
	if dir == "" {
		return
	}
	err := log.Follow(ctx, dir, time.Second, log.Filter{}, func(e log.Entry) error {
		h.Publish(TopicLog, e.Level.String(), e.Message, toAPILog(e))
		return nil
	})
	if err != nil && ctx.Err() == nil {
		log.Warn("events: follow the log: %s", err)
	}
}

func toAPILog(e log.Entry) apiv1.LogEntry {
	return apiv1.LogEntry{Time: e.Time, Level: e.Level.String(), Message: e.Message, File: e.File}
}

// eventStream is the answer of GET /api/v1/events.
type eventStream struct {
	ctx    context.Context
	events <-chan Event
}

// parseTopics are the topics of the query, every one when empty.
func parseTopics(s string) ([]string, error) {
	list := splitList(s)
	if len(list) == 0 {
		return Topics, nil
	}
	for _, t := range list {
		if !slices.Contains(Topics, t) {
			return nil, badRequest("topics: %q isn't a topic (%s)", t, strings.Join(Topics, ", "))
		}
	}
	return list, nil
}

// GetEvents implements apiv1.StrictServerInterface.
func (a *API) GetEvents(ctx context.Context, request apiv1.GetEventsRequestObject) (apiv1.GetEventsResponseObject, error) {
	topics, err := parseTopics(deref(request.Params.Topics))
	if err != nil {
		return nil, err
	}
	// the health findings need manage, the log admin: left out for who hasn't
	id, _ := identityOf(ctx)
	topics = slices.DeleteFunc(slices.Clone(topics), func(t string) bool {
		return (t == TopicHealth && !id.has(ScopeManage)) || (t == TopicLog && !id.has(ScopeAdmin))
	})
	var since uint64
	if s := deref(request.Params.Since); s > 0 {
		since = uint64(s)
	} else if id, ok := lastEventID(ctx); ok {
		since = id
	}
	if a.Events == nil {
		return nil, newAPIError(http.StatusServiceUnavailable, CodeUnavailable, "no events here")
	}
	return eventStream{ctx: ctx, events: a.Events.Subscribe(ctx, topics, since)}, nil
}

func (s eventStream) VisitGetEventsResponse(w http.ResponseWriter) error {
	rc := http.NewResponseController(w)
	startStream(w)
	if _, err := io.WriteString(w, ": mangal serve events\n\n"); err != nil {
		return nil
	}
	_ = rc.Flush()
	ping := time.NewTicker(eventsPing)
	defer ping.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return nil
		case <-ping.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return nil
			}
		case e, ok := <-s.events:
			if !ok {
				return nil
			}
			b, err := json.Marshal(e)
			if err != nil {
				return nil
			}
			if _, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", e.Seq, b); err != nil {
				return nil
			}
		}
		if err := rc.Flush(); err != nil {
			return nil
		}
	}
}

type lastEventIDKey struct{}

// lastEventID is the request's Last-Event-ID (a reconnection), put in its ctx by the app.
func lastEventID(ctx context.Context) (uint64, bool) {
	id, ok := ctx.Value(lastEventIDKey{}).(uint64)
	return id, ok
}

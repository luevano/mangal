package serve

import (
	"cmp"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/storage"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// runs tracks the check runs of the server: the running ones and how to cancel the ones
// asked through the API.
type runs struct {
	mu      sync.Mutex
	current map[int64]bool
	cancels map[int64]context.CancelFunc
}

func (r *runs) start(id int64, cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.current == nil {
		r.current = map[int64]bool{}
	}
	r.current[id] = true
	if cancel != nil {
		if r.cancels == nil {
			r.cancels = map[int64]context.CancelFunc{}
		}
		r.cancels[id] = cancel
	}
}

func (r *runs) done(id int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.current, id)
	delete(r.cancels, id)
}

// running are the running check runs, by id.
func (r *runs) running() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := slices.Collect(maps.Keys(r.current))
	slices.Sort(ids)
	return ids
}

func (r *runs) cancel(id int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	cancel, ok := r.cancels[id]
	if ok {
		cancel()
	}
	return ok
}

// API is the JSON API of `mangal serve` over a watch manager, see watch.Remote for its
// client and watch's AddRequest etc. for the bodies.
type API struct {
	// Manager is the manager to use for a request.
	Manager func() watch.Manager
	// Info is the server part of the status.
	Info watch.Status
	// Token, when set, is required as a bearer token.
	Token string
	// Changed is told when watches are added or changed (to wake the scheduler), may be nil.
	Changed func()
	Log     logger
	Runs    *runs

	// ctx is the server's, the checks asked through the API run until it's cancelled
	ctx    context.Context
	checks sync.WaitGroup
}

// Wait waits for the checks asked through the API to finish.
func (a *API) Wait() { a.checks.Wait() }

// Register adds the API's routes (all under /api/) to the mux.
func (a *API) Register(mux *http.ServeMux) {
	handle := func(pattern string, h func(http.ResponseWriter, *http.Request) error) {
		mux.Handle(pattern, a.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := h(w, r); err != nil {
				writeError(w, err)
			}
		})))
	}
	handle("GET /api/status", a.status)
	handle("GET /api/watches", a.list)
	handle("POST /api/watches", a.add)
	handle("GET /api/watches/stale", a.stale)
	handle("GET /api/watches/{id}", a.get)
	handle("PATCH /api/watches/{id}", a.patch)
	handle("DELETE /api/watches/{id}", a.remove)
	handle("POST /api/watches/{id}/relink", a.relink)
	handle("POST /api/check", a.check)
	handle("GET /api/runs", a.runs)
	handle("GET /api/runs/{id}", a.run)
	handle("POST /api/runs/{id}/cancel", a.cancel)
	handle("DELETE /api/watch-runs/{id}", a.removeRun)
	handle("GET /api/events", a.events)
}

func (a *API) auth(next http.Handler) http.Handler {
	if a.Token == "" {
		return next
	}
	want := []byte("Bearer " + a.Token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(w, http.StatusUnauthorized, watch.ErrorResponse{Error: "missing or wrong token (serve.token)"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// httpError is an error with its status code.
type httpError struct {
	status int
	err    error
}

func (e *httpError) Error() string { return e.err.Error() }
func (e *httpError) Unwrap() error { return e.err }

func badRequest(format string, a ...any) error {
	return &httpError{http.StatusBadRequest, fmt.Errorf(format, a...)}
}

func writeError(w http.ResponseWriter, err error) {
	code := watch.ErrorCode(err)
	status := http.StatusInternalServerError
	var he *httpError
	switch {
	case errors.As(err, &he):
		status = he.status
	case code == watch.CodeNotFound:
		status = http.StatusNotFound
	case code == watch.CodeExists, code == watch.CodeBusy:
		status = http.StatusConflict
	}
	writeJSON(w, status, watch.ErrorResponse{Error: err.Error(), Code: code})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decode(r *http.Request, v any) error {
	// a page of another site can only send simple content types without a CORS preflight
	if mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mediaType != "application/json" {
		return &httpError{http.StatusUnsupportedMediaType, errors.New("the body has to be JSON (Content-Type: application/json)")}
	}
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return badRequest("decode the body: %w", err)
	}
	return nil
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, badRequest("%q isn't an id", r.PathValue("id"))
	}
	return id, nil
}

// orEmpty makes a nil list [] in JSON.
func orEmpty[T any](list []T) []T {
	if list == nil {
		return []T{}
	}
	return list
}

func (a *API) changed() {
	if a.Changed != nil {
		a.Changed()
	}
}

func (a *API) status(w http.ResponseWriter, r *http.Request) error {
	st, err := a.Manager().Status(r.Context())
	if err != nil {
		return err
	}
	st.Server, st.Address, st.Version, st.Started = true, a.Info.Address, a.Info.Version, a.Info.Started
	st.Runs = a.Runs.running()
	if len(st.Runs) > 0 {
		st.Run = st.Runs[0]
	}
	writeJSON(w, http.StatusOK, st)
	return nil
}

func (a *API) list(w http.ResponseWriter, r *http.Request) error {
	list, err := a.Manager().List(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, orEmpty(list))
	return nil
}

func (a *API) add(w http.ResponseWriter, r *http.Request) error {
	var req watch.AddRequest
	if err := decode(r, &req); err != nil {
		return err
	}
	if req.Provider == "" || (req.MangaID == "" && req.Query == "") {
		return badRequest("provider and manga_id or query are needed")
	}
	if _, err := watch.ParseSchedule(req.Schedule); err != nil {
		return badRequest("%w", err)
	}
	added, err := a.Manager().Add(r.Context(), req.Options())
	if err != nil {
		if watch.ErrorCode(err) == "" {
			// the provider or the manga, not the server
			err = &httpError{http.StatusUnprocessableEntity, err}
		}
		return err
	}
	a.Log.Infof("watching %q from %s (id %d)", added.Title, added.Provider, added.ID)
	a.changed()
	writeJSON(w, http.StatusCreated, added)
	return nil
}

func (a *API) stale(w http.ResponseWriter, r *http.Request) error {
	since := 90 * 24 * time.Hour
	if s := r.URL.Query().Get("since"); s != "" {
		var err error
		if since, err = parseDuration(s); err != nil {
			return badRequest("since: %w", err)
		}
	}
	list, err := a.Manager().Stale(r.Context(), since)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, orEmpty(list))
	return nil
}

func (a *API) get(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	got, err := a.Manager().Get(r.Context(), id)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, got)
	return nil
}

func (a *API) patch(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	var req watch.PatchRequest
	if err := decode(r, &req); err != nil {
		return err
	}
	update, changes := req.Update()
	if req.Enabled == nil && !changes {
		return badRequest("nothing to change")
	}
	if update.Schedule != nil {
		if _, err := watch.ParseSchedule(*update.Schedule); err != nil {
			return badRequest("%w", err)
		}
	}
	m := a.Manager()
	var got watch.Watch
	if req.Enabled != nil {
		if got, err = m.SetEnabled(r.Context(), id, *req.Enabled); err != nil {
			return err
		}
	}
	if changes {
		if got, err = m.Update(r.Context(), id, update); err != nil {
			return err
		}
	}
	a.changed()
	writeJSON(w, http.StatusOK, got)
	return nil
}

func (a *API) relink(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	var req watch.RelinkOptions
	if err := decode(r, &req); err != nil {
		return err
	}
	if req.MangaID == "" && req.Query == "" {
		return badRequest("manga_id or query is needed")
	}
	got, err := a.Manager().Relink(r.Context(), id, req)
	if err != nil {
		if watch.ErrorCode(err) == "" {
			// the provider or the manga, not the server
			err = &httpError{http.StatusUnprocessableEntity, err}
		}
		return err
	}
	a.Log.Infof("re-linked watch %d to %q from %s (%s)", got.ID, got.Title, got.Provider, got.MangaID)
	a.changed()
	writeJSON(w, http.StatusOK, got)
	return nil
}

func (a *API) remove(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := a.Manager().Remove(r.Context(), id); err != nil {
		return err
	}
	a.Log.Infof("stopped watching %d", id)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *API) removeRun(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	removed, err := a.Manager().RemoveRun(r.Context(), id)
	if err != nil {
		return err
	}
	a.Log.Infof("removed watch run %d from the history (snapshot %s)", id, removed.Snapshot)
	writeJSON(w, http.StatusOK, removed)
	return nil
}

func (a *API) check(w http.ResponseWriter, r *http.Request) error {
	var req watch.CheckRequest
	if err := decode(r, &req); err != nil {
		return err
	}
	kind := watch.KindManual
	if req.Gaps {
		kind = watch.KindGaps
	}
	m := a.Manager()
	if req.DryRun {
		report, err := m.Check(r.Context(), req.IDs, watch.CheckOptions{Kind: kind, Gaps: req.Gaps, DryRun: true})
		if err != nil {
			return err
		}
		report.Results = orEmpty(report.Results)
		writeJSON(w, http.StatusOK, report)
		return nil
	}

	// runs in the server, not cancelled with the request
	ctx, cancel := context.WithCancel(a.ctx)
	started := make(chan watch.CheckResponse, 1)
	failed := make(chan error, 1)
	ended := make(chan struct{})
	a.checks.Add(1)
	go func() {
		defer a.checks.Done()
		defer close(ended)
		defer cancel()
		var id int64
		var busy []string
		report, err := m.Check(ctx, req.IDs, watch.CheckOptions{Kind: kind, Gaps: req.Gaps,
			OnEvent: logEvents(a.Log, false),
			OnBusy:  func(providers []string) { busy = providers },
			OnRun: func(run storage.CheckRun) {
				id = run.ID
				a.Runs.start(id, cancel)
				what := "every enabled watch"
				if len(req.IDs) > 0 {
					what = stringutil.Quantify(len(req.IDs), "watch", "watches")
				}
				if len(busy) > 0 {
					what += " (busy, skipped: " + strings.Join(busy, ", ") + ")"
				}
				a.Log.Infof("%s check %d (asked through the API): %s", run.Kind, id, what)
				started <- watch.CheckResponse{Run: id, Busy: busy}
			},
		})
		if id != 0 {
			a.Runs.done(id)
		}
		switch {
		case err != nil && id == 0:
			failed <- err
		case err != nil:
			a.Log.Errorf("%s check %d: %s", kind, id, err)
		default:
			logReport(a.Log, report)
		}
	}()
	select {
	case resp := <-started:
		writeJSON(w, http.StatusAccepted, resp)
		return nil
	case err := <-failed:
		return err
	case <-ended:
		select {
		case resp := <-started:
			writeJSON(w, http.StatusAccepted, resp)
			return nil
		case err := <-failed:
			return err
		default:
			return errors.New("the check didn't start")
		}
	}
}

func (a *API) runs(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	var filter watch.RunFilter
	if s := q.Get("since"); s != "" {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			filter.Since = t
		} else if d, err := parseDuration(s); err == nil {
			filter.Since = time.Now().Add(-d)
		} else {
			return badRequest("since: %q isn't a time (RFC 3339) or a duration (7d, 12h)", s)
		}
	}
	if s := q.Get("watch"); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n < 0 {
			return badRequest("watch: %q isn't an id", s)
		}
		filter.Watch = n
	}
	filter.Provider, filter.MangaID = q.Get("provider"), q.Get("manga_id")
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return badRequest("limit: %q isn't a number", s)
		}
		filter.Limit = n
	}
	list, err := a.Manager().Runs(r.Context(), filter)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, orEmpty(list))
	return nil
}

func (a *API) run(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	run, err := a.Manager().RunDetails(r.Context(), id)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, run)
	return nil
}

func (a *API) cancel(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if !a.Runs.cancel(id) {
		return fmt.Errorf("check run %d isn't running here (only the ones asked through the API can be cancelled): %w", id, watch.ErrNotFound)
	}
	a.Log.Infof("check %d cancelled through the API", id)
	w.WriteHeader(http.StatusAccepted)
	return nil
}

// eventsPing is how often the event stream sends a keep-alive.
var eventsPing = 30 * time.Second

// events streams the live events of the checks as server-sent events (id: the event's
// Seq, data: its JSON): the recent ones of the running checks first, after the Seq in
// ?since= or Last-Event-ID (a reconnection), then each one as it happens.
func (a *API) events(w http.ResponseWriter, r *http.Request) error {
	var since uint64
	if s := cmp.Or(r.URL.Query().Get("since"), r.Header.Get("Last-Event-ID")); s != "" {
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return badRequest("since: %q isn't an event id", s)
		}
		since = n
	}
	m := a.Manager()
	if svc, ok := m.(*watch.Service); ok && svc.Events != nil && since > svc.Events.Seq() {
		// the server restarted since: everything is new
		since = 0
	}
	events, err := m.Subscribe(r.Context())
	if err != nil {
		return err
	}
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if _, err := io.WriteString(w, ": mangal serve events\n\n"); err != nil {
		return nil
	}
	_ = rc.Flush()
	ping := time.NewTicker(eventsPing)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return nil
		case <-ping.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return nil
			}
		case e, ok := <-events:
			if !ok {
				// too far behind: the client reconnects from its last event
				return nil
			}
			if e.Seq <= since {
				continue
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

// parseDuration is a duration that also takes days ("7d", "1.5d").
func parseDuration(s string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.ParseFloat(days, 64)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("%q isn't a number of days", s)
		}
		return time.Duration(n * float64(24*time.Hour)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("%q isn't a duration (e.g. 7d, 12h)", s)
	}
	return d, nil
}

package serve

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/luevano/mangal/v5/log"
	"io"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/health"
	"github.com/luevano/mangal/v5/storage"
	stringutil "github.com/luevano/mangal/v5/util/string"
	"github.com/luevano/mangal/v5/web"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// APIVersion is the version of the API v1 (the spec's).
const APIVersion = "1.0.0"

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

// API is the HTTP API of `mangal serve` (web/openapi.yaml): the API v1 (apiv1's strict
// server) over the watch manager, the app's services (core), the jobs and the events;
// see NewApp for its routes.
type API struct {
	// Manager is the manager to use for a request.
	Manager func() watch.Manager
	// Info is the server part of the status.
	Info watch.Status
	// Auth authenticates the requests.
	Auth *Auth
	// Users are the users and API tokens of the admin page, core.NewUsers() when nil.
	Users core.Users
	// Changed is told when watches are added or changed (to wake the scheduler), may be nil.
	Changed func()
	Runs    *runs
	// Health makes the health checker over the watches of a manager (health.NewWith when
	// nil), Core the app's services over them (core.New with them and Health when nil).
	Health func(watch.Manager) (health.Checker, error)
	Core   func(watch.Manager) (*core.Core, error)
	// Browser has the providers and the image proxy (and the web UI's browse routes).
	Browser *web.Server
	// Jobs are the server's downloads and scans, Events the events of /api/v1/events.
	Jobs   *jobs.Manager
	Events *Events

	healthJobs healthJobs
	summary    healthSummary
	trackers   trackerLogins
	kindsOnce  sync.Once
	jobKinds   core.JobKinds
	wakeKinds  waker
	online     onlinePages
	coreOnce   sync.Mutex
	core       *core.Core

	// ctx is the server's, the checks asked through the API run until it's cancelled
	ctx    context.Context
	checks sync.WaitGroup
	// stopping refuses new checks once Wait is called
	mu       sync.Mutex
	stopping bool
}

var _ apiv1.StrictServerInterface = (*API)(nil)

// Wait refuses new checks and waits for the ones asked through the API to finish.
func (a *API) Wait() {
	a.mu.Lock()
	a.stopping = true
	a.mu.Unlock()
	a.checks.Wait()
}

// startCheck counts a check in, false once the server is stopping.
func (a *API) startCheck() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopping || a.ctx.Err() != nil {
		return false
	}
	a.checks.Add(1)
	return true
}

// services are the app's services over the server's watches, made once.
func (a *API) services(ctx context.Context) (*core.Core, error) {
	a.coreOnce.Lock()
	defer a.coreOnce.Unlock()
	if a.core != nil {
		return a.core, nil
	}
	newCore := a.Core
	if newCore == nil {
		newCore = func(m watch.Manager) (*core.Core, error) {
			return core.New(ctx, core.Env{Watches: func(context.Context) (watch.Manager, error) { return a.Manager(), nil }, Checker: a.Health})
		}
	}
	c, err := newCore(a.Manager())
	if err != nil {
		return nil, err
	}
	a.core = c
	return c, nil
}

// orEmpty makes a nil list [] in JSON.
func orEmpty[T any](list []T) []T {
	if list == nil {
		return []T{}
	}
	return list
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

func (a *API) changed() {
	if a.Changed != nil {
		a.Changed()
	}
}

// GetIndex implements apiv1.StrictServerInterface.
func (a *API) GetIndex(context.Context, apiv1.GetIndexRequestObject) (apiv1.GetIndexResponseObject, error) {
	ops, err := web.Operations()
	if err != nil {
		return nil, err
	}
	index := apiv1.GetIndex200JSONResponse{ApiVersion: APIVersion, Version: a.Info.Version, Operations: []apiv1.IndexOperation{}}
	for _, o := range ops {
		if !o.V1 {
			continue
		}
		plan, admin := o.Plan, o.Scope == ScopeAdmin
		index.Operations = append(index.Operations, apiv1.IndexOperation{Method: o.Method, Path: o.Path, OperationId: o.ID,
			Scope: o.Scope, Plan: &plan, Admin: &admin, Summary: o.Summary})
	}
	return index, nil
}

// GetSpec implements apiv1.StrictServerInterface.
func (a *API) GetSpec(context.Context, apiv1.GetSpecRequestObject) (apiv1.GetSpecResponseObject, error) {
	return apiv1.GetSpec200ApplicationyamlResponse{Body: strings.NewReader(string(web.Spec)), ContentLength: int64(len(web.Spec))}, nil
}

// GetStatus implements apiv1.StrictServerInterface.
func (a *API) GetStatus(ctx context.Context, _ apiv1.GetStatusRequestObject) (apiv1.GetStatusResponseObject, error) {
	st, err := a.Manager().Status(ctx)
	if err != nil {
		return nil, err
	}
	resp := apiv1.GetStatus200JSONResponse{
		Server: true, Address: &a.Info.Address, Version: &a.Info.Version, Started: &a.Info.Started,
		Checking: st.Checking, Providers: new(orEmpty(st.Providers)), NextCheck: optTime(st.NextCheck), Since: optTime(st.Since),
		ApiVersion: APIVersion, Features: core.APIFeatures(), Scopes: []string{},
	}
	runs := a.Runs.running()
	if len(runs) > 0 {
		resp.Runs, resp.Run = &runs, &runs[0]
	}
	if id, ok := identityOf(ctx); ok {
		resp.Scopes, resp.Auth = id.Scopes, id.String()
	}
	if a.Events != nil {
		resp.EventSeq = new(a.Events.Seq())
	}
	if keys := restartNeeded(ctx); len(keys) > 0 {
		resp.RestartNeeded = &keys
	}
	resp.Health = a.summary.get(a)
	return resp, nil
}

// healthSummary is the library's health in short for the status (anyone with read sees
// it), from the local checks run in the background at most every healthSummaryEvery.
type healthSummary struct {
	mu      sync.Mutex
	last    *apiv1.HealthSummary
	running bool
}

const healthSummaryEvery = 5 * time.Minute

// get is the last summary (nil before the first), starting a new one when it's old.
func (h *healthSummary) get(a *API) *apiv1.HealthSummary {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.running && (h.last == nil || time.Since(h.last.Checked) > healthSummaryEvery) {
		h.running = true
		go func() {
			sum := a.runSummary()
			h.mu.Lock()
			defer h.mu.Unlock()
			h.running = false
			if sum != nil {
				h.last = sum
			}
		}()
	}
	return h.last
}

func (a *API) runSummary() *apiv1.HealthSummary {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	c, err := a.services(ctx)
	if err != nil {
		return nil
	}
	report, err := c.Health.Report(ctx, health.Options{Kept: a.healthJobs.sourceFindings()})
	if err != nil {
		log.Warn("health summary: %s", err)
		return nil
	}
	return &apiv1.HealthSummary{Warnings: report.Counts.Warning, Errors: report.Counts.Error, Checked: time.Now()}
}

func optTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// ListWatches implements apiv1.StrictServerInterface.
func (a *API) ListWatches(ctx context.Context, _ apiv1.ListWatchesRequestObject) (apiv1.ListWatchesResponseObject, error) {
	list, err := a.Manager().List(ctx)
	if err != nil {
		return nil, err
	}
	return apiv1.ListWatches200JSONResponse(orEmpty(list)), nil
}

// AddWatch implements apiv1.StrictServerInterface.
func (a *API) AddWatch(ctx context.Context, request apiv1.AddWatchRequestObject) (apiv1.AddWatchResponseObject, error) {
	req := deref(request.Body)
	if req.Provider == "" || (req.MangaID == "" && req.Query == "") {
		return nil, badRequest("provider and manga_id or query are needed")
	}
	if _, err := watch.ParseSchedule(req.Schedule); err != nil {
		return nil, badRequest("%w", err)
	}
	added, err := a.Manager().Add(ctx, req.Options())
	if err != nil {
		return nil, unprocessable(err)
	}
	log.Info("watching %q from %s (id %d)", added.Title, added.Provider, added.ID)
	a.changed()
	return apiv1.AddWatch201JSONResponse(added), nil
}

// ListStaleWatches implements apiv1.StrictServerInterface.
func (a *API) ListStaleWatches(ctx context.Context, request apiv1.ListStaleWatchesRequestObject) (apiv1.ListStaleWatchesResponseObject, error) {
	since := 90 * 24 * time.Hour
	if s := deref(request.Params.Since); s != "" {
		var err error
		if since, err = parseDuration(s); err != nil {
			return nil, badRequest("since: %w", err)
		}
	}
	list, err := a.Manager().Stale(ctx, since)
	if err != nil {
		return nil, err
	}
	return apiv1.ListStaleWatches200JSONResponse(orEmpty(list)), nil
}

// GetWatch implements apiv1.StrictServerInterface.
func (a *API) GetWatch(ctx context.Context, request apiv1.GetWatchRequestObject) (apiv1.GetWatchResponseObject, error) {
	got, err := a.Manager().Get(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	return apiv1.GetWatch200JSONResponse(got), nil
}

// UpdateWatch implements apiv1.StrictServerInterface.
func (a *API) UpdateWatch(ctx context.Context, request apiv1.UpdateWatchRequestObject) (apiv1.UpdateWatchResponseObject, error) {
	req := deref(request.Body)
	update, changes := req.Update()
	if req.Enabled == nil && !changes {
		return nil, badRequest("nothing to change")
	}
	if update.Schedule != nil {
		if _, err := watch.ParseSchedule(*update.Schedule); err != nil {
			return nil, badRequest("%w", err)
		}
	}
	m := a.Manager()
	var got watch.Watch
	var err error
	if req.Enabled != nil {
		if got, err = m.SetEnabled(ctx, request.Id, *req.Enabled); err != nil {
			return nil, err
		}
	}
	if changes {
		if got, err = m.Update(ctx, request.Id, update); err != nil {
			return nil, err
		}
	}
	a.changed()
	return apiv1.UpdateWatch200JSONResponse(got), nil
}

// RelinkWatch implements apiv1.StrictServerInterface.
func (a *API) RelinkWatch(ctx context.Context, request apiv1.RelinkWatchRequestObject) (apiv1.RelinkWatchResponseObject, error) {
	req := deref(request.Body)
	if req.MangaID == "" && req.Query == "" {
		return nil, badRequest("manga_id or query is needed")
	}
	got, err := a.Manager().Relink(ctx, request.Id, req)
	if err != nil {
		return nil, unprocessable(err)
	}
	log.Info("re-linked watch %d to %q from %s (%s)", got.ID, got.Title, got.Provider, got.MangaID)
	a.changed()
	return apiv1.RelinkWatch200JSONResponse(got), nil
}

// HoldWatch implements apiv1.StrictServerInterface.
func (a *API) HoldWatch(ctx context.Context, request apiv1.HoldWatchRequestObject) (apiv1.HoldWatchResponseObject, error) {
	res, err := a.hold(ctx, watch.HoldOptions{Watch: request.Id}, deref(request.Body))
	if err != nil {
		return nil, err
	}
	return apiv1.HoldWatch200JSONResponse(res), nil
}

// HoldLibraryManga implements apiv1.StrictServerInterface.
func (a *API) HoldLibraryManga(ctx context.Context, request apiv1.HoldLibraryMangaRequestObject) (apiv1.HoldLibraryMangaResponseObject, error) {
	res, err := a.hold(ctx, watch.HoldOptions{Manga: request.Id}, deref(request.Body))
	if err != nil {
		return nil, err
	}
	return apiv1.HoldLibraryManga200JSONResponse(res), nil
}

func (a *API) hold(ctx context.Context, opts watch.HoldOptions, req watch.HoldRequest) (watch.HoldResult, error) {
	if req.Mark != "" && !slices.Contains(watch.Marks, req.Mark) {
		return watch.HoldResult{}, badRequest("mark %q: not one of %s", req.Mark, strings.Join(watch.Marks, ", "))
	}
	opts.Mark, opts.Reason, opts.NoCheck = req.Mark, req.Reason, req.NoCheck
	res, err := a.Manager().Hold(ctx, opts)
	if err != nil {
		return res, err
	}
	what := fmt.Sprintf("watch %d", opts.Watch)
	if opts.Manga != 0 {
		what = fmt.Sprintf("library manga %d", opts.Manga)
	}
	log.Info("marked %s %s", what, cmp.Or(req.Mark, "(cleared)"))
	a.changed()
	return res, nil
}

// RemoveWatch implements apiv1.StrictServerInterface.
func (a *API) RemoveWatch(ctx context.Context, request apiv1.RemoveWatchRequestObject) (apiv1.RemoveWatchResponseObject, error) {
	if err := a.Manager().Remove(ctx, request.Id); err != nil {
		return nil, err
	}
	log.Info("stopped watching %d", request.Id)
	return apiv1.RemoveWatch204Response{}, nil
}

// RemoveWatchRun implements apiv1.StrictServerInterface.
func (a *API) RemoveWatchRun(ctx context.Context, request apiv1.RemoveWatchRunRequestObject) (apiv1.RemoveWatchRunResponseObject, error) {
	removed, err := a.Manager().RemoveRun(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	log.Info("removed watch run %d from the history (snapshot %s)", request.Id, removed.Snapshot)
	return apiv1.RemoveWatchRun200JSONResponse(removed), nil
}

// CheckWatches implements apiv1.StrictServerInterface.
func (a *API) CheckWatches(ctx context.Context, request apiv1.CheckWatchesRequestObject) (apiv1.CheckWatchesResponseObject, error) {
	req := deref(request.Body)
	kind := watch.KindManual
	switch {
	case req.Gaps:
		kind = watch.KindGaps
	case req.Hold:
		kind = watch.KindHold
	}
	m := a.Manager()
	if req.DryRun {
		report, err := m.Check(ctx, req.IDs, watch.CheckOptions{Kind: kind, Gaps: req.Gaps, DryRun: true})
		if err != nil {
			return nil, err
		}
		report.Results = orEmpty(report.Results)
		return apiv1.CheckWatches200JSONResponse(report), nil
	}

	// runs in the server, not cancelled with the request
	checkCtx, cancel := context.WithCancel(a.ctx)
	started := make(chan watch.CheckResponse, 1)
	failed := make(chan error, 1)
	ended := make(chan struct{})
	if !a.startCheck() {
		cancel()
		return nil, newAPIError(http.StatusServiceUnavailable, CodeUnavailable, "the server is stopping")
	}
	go func() {
		defer a.checks.Done()
		defer close(ended)
		defer cancel()
		var id int64
		var busy []string
		report, err := m.Check(checkCtx, req.IDs, watch.CheckOptions{Kind: kind, Gaps: req.Gaps,
			OnEvent: logEvents(false),
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
				log.Info("%s check %d (asked through the API): %s", run.Kind, id, what)
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
			log.Error("%s check %d: %s", kind, id, err)
		default:
			logReport(report)
		}
	}()
	select {
	case resp := <-started:
		return apiv1.CheckWatches202JSONResponse(resp), nil
	case err := <-failed:
		return nil, err
	case <-ended:
		select {
		case resp := <-started:
			return apiv1.CheckWatches202JSONResponse(resp), nil
		case err := <-failed:
			return nil, err
		default:
			return nil, errors.New("the check didn't start")
		}
	}
}

// ListRuns implements apiv1.StrictServerInterface.
func (a *API) ListRuns(ctx context.Context, request apiv1.ListRunsRequestObject) (apiv1.ListRunsResponseObject, error) {
	p := request.Params
	var filter watch.RunFilter
	if s := deref(p.Since); s != "" {
		t, err := parseSince(s)
		if err != nil {
			return nil, badRequest("since: %w", err)
		}
		filter.Since = t
	}
	if w := deref(p.Watch); w < 0 {
		return nil, badRequest("watch: %d isn't an id", w)
	} else {
		filter.Watch = w
	}
	filter.Provider, filter.MangaID, filter.Limit = deref(p.Provider), deref(p.MangaId), deref(p.Limit)
	list, err := a.Manager().Runs(ctx, filter)
	if err != nil {
		return nil, err
	}
	return apiv1.ListRuns200JSONResponse(orEmpty(list)), nil
}

// parseSince is a time (RFC 3339) or a duration ago (7d, 12h).
func parseSince(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	if d, err := parseDuration(s); err == nil {
		return time.Now().Add(-d), nil
	}
	return time.Time{}, fmt.Errorf("%q isn't a time (RFC 3339) or a duration (7d, 12h)", s)
}

// GetRun implements apiv1.StrictServerInterface.
func (a *API) GetRun(ctx context.Context, request apiv1.GetRunRequestObject) (apiv1.GetRunResponseObject, error) {
	run, err := a.Manager().RunDetails(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	return apiv1.GetRun200JSONResponse(run), nil
}

// CancelRun implements apiv1.StrictServerInterface.
func (a *API) CancelRun(_ context.Context, request apiv1.CancelRunRequestObject) (apiv1.CancelRunResponseObject, error) {
	if !a.Runs.cancel(request.Id) {
		return nil, fmt.Errorf("check run %d isn't running here (only the ones asked through the API can be cancelled): %w", request.Id, watch.ErrNotFound)
	}
	log.Info("check %d cancelled through the API", request.Id)
	return apiv1.CancelRun202Response{}, nil
}

// eventsPing is how often the event streams send a keep-alive.
var eventsPing = 30 * time.Second

// legacyEvents is GET /api/events: the live events of the checks (watch.LiveEvent, what
// watch.Remote reads) as server-sent events (id: the event's Seq, data: its JSON): the
// recent ones of the running checks first, after the Seq in ?since= or Last-Event-ID (a
// reconnection), then each one as it happens.
func (a *API) legacyEvents(c *echo.Context) error {
	r := c.Request()
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
	w := c.Response()
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

// startStream starts an event stream answer.
func startStream(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
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

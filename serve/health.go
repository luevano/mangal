package serve

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/luevano/libmangal/progress"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/health"
)

// keepHealthJobs is how many finished health jobs the server keeps the reports of.
const keepHealthJobs = 5

// HealthJob is a run of the health checks with the network ones (GET /api/health?network=1),
// in the background: its events go to /api/events (Job set), GET /api/health/jobs/{id}
// has its report once it's done.
type HealthJob struct {
	ID       int64     `json:"id"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished,omitzero"`
	Running  bool      `json:"running"`
	// Status is what it's doing while it runs.
	Status string         `json:"status,omitempty"`
	Error  string         `json:"error,omitempty"`
	Report *health.Report `json:"report,omitempty"`
}

// HealthFixRequest asks for the plan of a finding's fix (POST /api/health/fix), or to
// apply it.
type HealthFixRequest struct {
	// Key is the finding's (or a summary item's) key, e.g. "M1/manga/5".
	Key string `json:"key"`
	// Choice is the id to link when the fix has several (Fix.Action.IDs).
	Choice string `json:"choice,omitempty"`
	// Apply makes the change, else only its plan is answered.
	Apply bool `json:"apply,omitempty"`
}

// healthJobs are the health jobs of the server: one runs at a time.
type healthJobs struct {
	mu   sync.Mutex
	next int64
	list []*HealthJob
}

func (j *healthJobs) get(id int64) (HealthJob, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, job := range j.list {
		if job.ID == id {
			return *job, true
		}
	}
	return HealthJob{}, false
}

// running is the running job, if any.
func (j *healthJobs) running() (HealthJob, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, job := range j.list {
		if job.Running {
			return *job, true
		}
	}
	return HealthJob{}, false
}

// latest is the report of the last job that finished with one.
func (j *healthJobs) latest() (health.Report, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, job := range slices.Backward(j.list) {
		if job.Report != nil {
			return *job.Report, true
		}
	}
	return health.Report{}, false
}

// start adds a running job, false with the running one when there's one.
func (j *healthJobs) start() (HealthJob, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, job := range j.list {
		if job.Running {
			return *job, false
		}
	}
	j.next++
	job := &HealthJob{ID: j.next, Started: time.Now(), Running: true}
	j.list = append(j.list, job)
	if len(j.list) > keepHealthJobs+1 {
		j.list = j.list[len(j.list)-keepHealthJobs-1:]
	}
	return *job, true
}

func (j *healthJobs) update(id int64, do func(*HealthJob)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, job := range j.list {
		if job.ID == id {
			do(job)
		}
	}
}

// registerHealth adds the health routes.
func (a *API) registerHealth(handle func(pattern string, h func(http.ResponseWriter, *http.Request) error)) {
	handle("GET /api/health", a.health)
	handle("GET /api/health/jobs/{id}", a.healthJob)
	handle("POST /api/health/fix", a.healthFix)
}

// checker is the health checker over the server's watches.
func (a *API) checker() (health.Checker, error) {
	newChecker := a.Health
	if newChecker == nil {
		newChecker = health.NewWith
	}
	return newChecker(a.Manager())
}

// healthOptions are the options of the query: only (comma separated), all.
func healthOptions(r *http.Request) (health.Options, error) {
	q := r.URL.Query()
	var opts health.Options
	for _, name := range strings.Split(q.Get("only"), ",") {
		if name = strings.TrimSpace(name); name != "" {
			opts.Only = append(opts.Only, name)
		}
	}
	var err error
	if opts.All, err = queryBool(q.Get("all")); err != nil {
		return opts, badRequest("all: %w", err)
	}
	if opts.Network, err = queryBool(q.Get("network")); err != nil {
		return opts, badRequest("network: %w", err)
	}
	return opts, nil
}

func queryBool(s string) (bool, error) {
	if s == "" {
		return false, nil
	}
	return strconv.ParseBool(s)
}

// health answers the offline report; with network=1 it starts a job (or answers the
// running one) with 202.
func (a *API) health(w http.ResponseWriter, r *http.Request) error {
	opts, err := healthOptions(r)
	if err != nil {
		return err
	}
	checker, err := a.checker()
	if err != nil {
		return err
	}
	if !opts.Network {
		report, err := checker.Run(r.Context(), opts)
		if err != nil {
			return badRequest("%w", err)
		}
		report.Findings = orEmpty(report.Findings)
		writeJSON(w, http.StatusOK, report)
		return nil
	}
	// a bad only fails here, not in the job
	if err := health.Validate(opts); err != nil {
		return badRequest("%w", err)
	}
	job, ok := a.healthJobs.start()
	if !ok {
		writeJSON(w, http.StatusAccepted, job)
		return nil
	}
	if !a.startCheck() {
		a.healthJobs.update(job.ID, func(j *HealthJob) { j.Running, j.Finished, j.Error = false, time.Now(), "the server is stopping" })
		return &httpError{http.StatusServiceUnavailable, errors.New("the server is stopping")}
	}
	go a.runHealth(job.ID, checker, opts)
	w.Header().Set("Location", fmt.Sprintf("/api/health/jobs/%d", job.ID))
	writeJSON(w, http.StatusAccepted, job)
	return nil
}

// runHealth runs the job in the server (not cancelled with the request), its progress as
// events.
func (a *API) runHealth(id int64, checker health.Checker, opts health.Options) {
	defer a.checks.Done()
	publish := func(kind, msg string) {
		if svc, ok := a.Manager().(*watch.Service); ok && svc.Events != nil {
			svc.Events.Publish(watch.LiveEvent{Job: id, Kind: kind, Message: msg})
		}
	}
	status := func(msg string) {
		a.healthJobs.update(id, func(j *HealthJob) { j.Status = msg })
		publish(watch.LiveHealthStatus, "health: "+msg)
	}
	what := "with the network checks"
	if len(opts.Only) != 0 {
		what += " (" + strings.Join(opts.Only, ", ") + ")"
	}
	a.Log.Infof("health check %d (asked through the API): %s", id, what)
	publish(watch.LiveHealthStart, "health: checking "+what)
	checker.OnProgress = status
	ctx := progress.WithReporter(a.ctx, func(s progress.Status) {
		if !s.IsMinor() {
			status(s.String())
		}
	})
	report, err := checker.Run(ctx, opts)
	a.healthJobs.update(id, func(j *HealthJob) {
		j.Running, j.Finished, j.Status = false, time.Now(), ""
		if err != nil {
			j.Error = err.Error()
			return
		}
		report.Findings = orEmpty(report.Findings)
		j.Report = &report
	})
	if err != nil {
		a.Log.Errorf("health check %d: %s", id, err)
		publish(watch.LiveHealthDone, "health: "+err.Error())
		return
	}
	for _, e := range report.Errors {
		a.Log.Warnf("health check %d: %s", id, e)
	}
	done := fmt.Sprintf("health: %d errors, %d warnings, %d info", report.Counts.Error, report.Counts.Warning, report.Counts.Info)
	a.Log.Infof("health check %d done: %s", id, strings.TrimPrefix(done, "health: "))
	publish(watch.LiveHealthDone, done)
}

func (a *API) healthJob(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	job, ok := a.healthJobs.get(id)
	if !ok {
		return fmt.Errorf("health job %d isn't here (the server keeps the last %d): %w", id, keepHealthJobs, watch.ErrNotFound)
	}
	writeJSON(w, http.StatusOK, job)
	return nil
}

// healthFix plans (or applies) the fix of a finding there now: an offline run of its
// check, else the last network job's report.
func (a *API) healthFix(w http.ResponseWriter, r *http.Request) error {
	var req HealthFixRequest
	if err := decode(r, &req); err != nil {
		return err
	}
	check, subject, err := health.SplitKey(req.Key)
	if err != nil {
		return badRequest("%w", err)
	}
	key := check + "/" + subject
	checker, err := a.checker()
	if err != nil {
		return err
	}
	var f health.Finding
	ok := false
	if !health.IsNetwork(check) {
		report, err := checker.Run(r.Context(), health.Options{Only: []string{check}, All: true})
		if err != nil {
			return err
		}
		f, ok = report.Find(key)
	}
	if latest, found := a.healthJobs.latest(); found && (!ok || f.Fix.Action == nil) {
		if nf, ok2 := latest.Find(key); ok2 {
			f, ok = nf, true
		}
	}
	if !ok {
		return fmt.Errorf("no finding %s now (a network check's needs a GET /api/health?network=1 first): %w", req.Key, watch.ErrNotFound)
	}
	newCore := a.Core
	if newCore == nil {
		newCore = func(m watch.Manager) (*core.Core, error) {
			return core.New(r.Context(), core.Env{Watches: func(context.Context) (watch.Manager, error) { return m, nil }})
		}
	}
	c, err := newCore(a.Manager())
	if err != nil {
		return err
	}
	res, err := fixFinding(r.Context(), c, f, req.Choice, req.Apply)
	var notFixable *health.NotFixableError
	switch {
	case errors.As(err, &notFixable):
		return &httpError{http.StatusUnprocessableEntity, err}
	case errors.Is(err, health.ErrChoice):
		return badRequest("%w", err)
	case errors.Is(err, core.ErrBusy), errors.Is(err, core.ErrPlanChanged):
		return &httpError{http.StatusConflict, err}
	case err != nil:
		return err
	}
	res.Plan = orEmpty(res.Plan)
	if res.Applied {
		a.Log.Infof("health fix %s applied: %s", res.Key, res.Message)
		if res.Kind == health.ActionWatch || res.Kind == health.ActionResume {
			a.changed()
		}
	}
	writeJSON(w, http.StatusOK, res)
	return nil
}

// fixFinding plans the finding's fix, applies it with apply (when there's something to do).
func fixFinding(ctx context.Context, c *core.Core, f health.Finding, choice string, apply bool) (health.FixResult, error) {
	plan, err := c.Changes.Plan(ctx, core.HealthFixReq{Finding: f, Choice: choice})
	if err != nil {
		return health.FixResult{}, err
	}
	fix := plan.Detail.(health.FixResult)
	if !apply || plan.Nothing {
		return fix, nil
	}
	res, err := c.Changes.ApplyPlan(ctx, plan, core.ApplyOpts{NoWait: true})
	if err != nil {
		return health.FixResult{}, err
	}
	return res.Detail.(health.FixResult), nil
}

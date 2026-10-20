package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/web/apiv1"

	"github.com/luevano/libmangal/progress"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/health"
)

// keepHealthJobs is how many finished health jobs the server keeps the reports of.
const keepHealthJobs = 5

// HealthJob is a run of the health checks with the network ones (GET /api/v1/health?network=1),
// in the background: its events go to /api/events (Job set), GET /api/v1/health/jobs/{id}
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

	// only are the checks it ran (health.Options.Only)
	only []string
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

// findings are the findings of the jobs' reports, the newest job's first.
func (j *healthJobs) findings() []health.Finding {
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []health.Finding
	for _, job := range slices.Backward(j.list) {
		if job.Report != nil {
			out = append(out, job.Report.Findings...)
		}
	}
	return out
}

// sourceFindings are the L11 findings of the newest job that ran L11 (the scheduled
// source check, or a network one with it).
func (j *healthJobs) sourceFindings() []health.Finding {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, job := range slices.Backward(j.list) {
		ran := len(job.only) == 0 || slices.ContainsFunc(job.only, func(name string) bool {
			return strings.EqualFold(name, "L11") || strings.EqualFold(name, string(health.Library))
		})
		if job.Report == nil || !ran {
			continue
		}
		var out []health.Finding
		for _, f := range job.Report.Findings {
			if f.Check == "L11" {
				out = append(out, f)
			}
		}
		return out
	}
	return nil
}

// start adds a running job of the checks only, false with the running one when there's
// one.
func (j *healthJobs) start(only []string) (HealthJob, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, job := range j.list {
		if job.Running {
			return *job, false
		}
	}
	j.next++
	job := &HealthJob{ID: j.next, Started: time.Now(), Running: true, only: only}
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

// splitList is a comma separated list, trimmed.
func splitList(s string) []string {
	var list []string
	for item := range strings.SplitSeq(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			list = append(list, item)
		}
	}
	return list
}

// healthJobResponse is a health job (202) with its url.
type healthJobResponse struct {
	job HealthJob
}

func (r healthJobResponse) VisitGetHealthResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", fmt.Sprintf("/api/v1/health/jobs/%d", r.job.ID))
	w.WriteHeader(http.StatusAccepted)
	return json.NewEncoder(w).Encode(r.job)
}

// GetHealth implements apiv1.StrictServerInterface: the offline report (with the L11
// findings of the last source check that still hold); with network=1 it starts a job (or
// answers the running one) with 202.
func (a *API) GetHealth(ctx context.Context, request apiv1.GetHealthRequestObject) (apiv1.GetHealthResponseObject, error) {
	p := request.Params
	opts := health.Options{Only: splitList(deref(p.Only)), All: deref(p.All), Network: deref(p.Network)}
	c, err := a.services(ctx)
	if err != nil {
		return nil, err
	}
	if !opts.Network {
		opts.Kept = a.healthJobs.sourceFindings()
		report, err := c.Health.Report(ctx, opts)
		if err != nil {
			return nil, badRequest("%w", err)
		}
		report.Findings = orEmpty(report.Findings)
		return apiv1.GetHealth200JSONResponse(report), nil
	}
	// a bad only fails here, not in the job
	if err := health.Validate(opts); err != nil {
		return nil, badRequest("%w", err)
	}
	job, ok := a.healthJobs.start(opts.Only)
	if !ok {
		return healthJobResponse{job}, nil
	}
	if !a.startCheck() {
		a.healthJobs.update(job.ID, func(j *HealthJob) { j.Running, j.Finished, j.Error = false, time.Now(), "the server is stopping" })
		return nil, newAPIError(http.StatusServiceUnavailable, CodeUnavailable, "the server is stopping")
	}
	go a.runHealth(job.ID, c.Health, opts, false)
	return healthJobResponse{job}, nil
}

// runHealth runs the job in the server (not cancelled with the request), its progress as
// events; a scheduled one logs only its result.
func (a *API) runHealth(id int64, h core.Health, opts health.Options, scheduled bool) {
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
	if !scheduled {
		a.Log.Infof("health check %d (asked through the API): %s", id, what)
	}
	publish(watch.LiveHealthStart, "health: checking "+what)
	ctx := progress.WithReporter(a.ctx, func(s progress.Status) {
		if !s.IsMinor() {
			status(s.String())
		}
	})
	ctx = core.WithProgress(ctx, func(p library.Progress) { status(p.String()) })
	report, err := h.Report(ctx, opts)
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
	if scheduled {
		a.Log.Infof("health check %d (scheduled, %s) done: %s", id, strings.Join(opts.Only, ", "), strings.TrimPrefix(done, "health: "))
	} else {
		a.Log.Infof("health check %d done: %s", id, strings.TrimPrefix(done, "health: "))
	}
	publish(watch.LiveHealthDone, done)
}

// GetHealthJob implements apiv1.StrictServerInterface.
func (a *API) GetHealthJob(_ context.Context, request apiv1.GetHealthJobRequestObject) (apiv1.GetHealthJobResponseObject, error) {
	job, ok := a.healthJobs.get(request.Id)
	if !ok {
		return nil, fmt.Errorf("health job %d isn't here (the server keeps the last %d): %w", request.Id, keepHealthJobs, watch.ErrNotFound)
	}
	return healthJobResponse{job}, nil
}

func (r healthJobResponse) VisitGetHealthJobResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(r.job)
}

// finding is the finding with the key there now: an offline run of its check, else the
// last network job's report.
func (a *API) finding(ctx context.Context, key string) (health.Finding, error) {
	check, subject, err := health.SplitKey(key)
	if err != nil {
		return health.Finding{}, badRequest("%w", err)
	}
	key = check + "/" + subject
	c, err := a.services(ctx)
	if err != nil {
		return health.Finding{}, err
	}
	var f health.Finding
	ok := false
	if !health.IsNetwork(check) {
		report, err := c.Health.Report(ctx, health.Options{Only: []string{check}, All: true})
		if err != nil {
			return f, err
		}
		f, ok = report.Find(key)
	}
	if !ok || f.Fix.Action == nil {
		if nf, ok2 := (health.Report{Findings: a.healthJobs.findings()}).Find(key); ok2 {
			f, ok = nf, true
		}
	}
	if !ok {
		return f, fmt.Errorf("no finding %s now (a network check's needs a GET /api/v1/health?network=1 first): %w", key, watch.ErrNotFound)
	}
	return f, nil
}

// FixHealthFinding implements apiv1.StrictServerInterface.
func (a *API) FixHealthFinding(ctx context.Context, request apiv1.FixHealthFindingRequestObject) (apiv1.FixHealthFindingResponseObject, error) {
	body := deref(request.Body)
	f, err := a.finding(ctx, body.Key)
	if err != nil {
		return nil, err
	}
	resp, err := a.change(ctx, core.HealthFixReq{Finding: f, Choice: deref(body.Choice)}, deref(request.Params.Apply),
		applyFields{body.Expect, body.Force, body.Confirm})
	if err != nil {
		return nil, err
	}
	if resp.Applied && (f.Fix.Action.Kind == health.ActionWatch || f.Fix.Action.Kind == health.ActionResume || f.Fix.Action.Kind == health.ActionHold) {
		a.changed()
	}
	return apiv1.FixHealthFinding200JSONResponse{ChangeJSONResponse: apiv1.ChangeJSONResponse(resp)}, nil
}

// DismissHealthFindings implements apiv1.StrictServerInterface: the findings to dismiss
// must be there now.
func (a *API) DismissHealthFindings(ctx context.Context, request apiv1.DismissHealthFindingsRequestObject) (apiv1.DismissHealthFindingsResponseObject, error) {
	body := deref(request.Body)
	c, err := a.services(ctx)
	if err != nil {
		return nil, err
	}
	resp := apiv1.DismissHealthFindings200JSONResponse{Dismissed: []string{}, Undismissed: []string{}}
	for _, key := range deref(body.Undismiss) {
		if err := c.Health.Undismiss(ctx, key); err != nil {
			return nil, err
		}
		resp.Undismissed = append(resp.Undismissed, key)
	}
	dismiss := deref(body.Dismiss)
	if len(dismiss) == 0 {
		return resp, nil
	}
	// the source check's findings are in the offline report: dismissed as they are
	req := core.DismissReq{Keys: dismiss, Network: deref(body.Network), Known: a.healthJobs.sourceFindings(), Fallback: a.healthJobs.findings()}
	dismissed, err := c.Health.Dismiss(ctx, req)
	for _, f := range dismissed {
		a.Log.Infof("health finding %s dismissed through the API: %s", f.Key, f.Message)
		resp.Dismissed = append(resp.Dismissed, f.Key)
	}
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// legacyHealthFix is POST /api/health/fix {"key", "choice", "apply"}: the plan of a
// finding's fix (health.FixResult), or its change when apply (no plan hash).
func (a *API) legacyHealthFix(c *echo.Context) error {
	var req HealthFixRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	f, err := a.finding(ctx, req.Key)
	if err != nil {
		return err
	}
	cr, err := a.services(ctx)
	if err != nil {
		return err
	}
	res, err := fixFinding(ctx, cr, f, req.Choice, req.Apply)
	if err != nil {
		return err
	}
	res.Plan = orEmpty(res.Plan)
	if res.Applied {
		a.Log.Infof("health fix %s applied: %s", res.Key, res.Message)
		if res.Kind == health.ActionWatch || res.Kind == health.ActionResume || res.Kind == health.ActionHold {
			a.changed()
		}
	}
	return c.JSON(http.StatusOK, res)
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

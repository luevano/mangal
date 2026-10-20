package serve

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/health"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// kinds are the server's job kinds (the Jobs page) over its jobs, made once.
func (a *API) kinds(ctx context.Context) (core.JobKinds, error) {
	m, err := a.jobs()
	if err != nil {
		return nil, err
	}
	c, err := a.services(ctx)
	if err != nil {
		return nil, err
	}
	a.kindsOnce.Do(func() {
		a.jobKinds = c.Kinds(m, core.KindsOptions{Origin: "api", Started: a.Info.Started, OnHealth: a.keepHealth, Changed: a.kindsChanged})
	})
	return a.jobKinds, nil
}

// kindsChanged wakes the schedules: a kind's settings or pause changed.
func (a *API) kindsChanged() {
	if a.Changed != nil {
		a.Changed()
	}
	a.wakeKinds.wake()
}

// keepHealth keeps a job kind's health run (a source check, a network health check) as a
// finished health job: GET /api/v1/health/jobs/{id} has it, and a source check's L11
// findings join the offline report.
func (a *API) keepHealth(opts health.Options, started time.Time, report health.Report, err error) int64 {
	job := a.healthJobs.add(opts.Only, started, report, err)
	what := "with the network checks"
	if len(opts.Only) != 0 {
		what = strings.Join(opts.Only, ", ")
	}
	if err != nil {
		a.Log.Errorf("health check %d (%s): %s", job.ID, what, err)
		return job.ID
	}
	for _, e := range report.Errors {
		a.Log.Warnf("health check %d: %s", job.ID, e)
	}
	a.Log.Infof("health check %d (%s) done: %d errors, %d warnings, %d info", job.ID, what, report.Counts.Error, report.Counts.Warning, report.Counts.Info)
	return job.ID
}

// add keeps a finished job of the checks only.
func (j *healthJobs) add(only []string, started time.Time, report health.Report, err error) HealthJob {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.next++
	job := &HealthJob{ID: j.next, Started: started, Finished: time.Now(), only: only}
	if err != nil {
		job.Error = err.Error()
	} else {
		report.Findings = orEmpty(report.Findings)
		job.Report = &report
	}
	j.list = append(j.list, job)
	if len(j.list) > keepHealthJobs+1 {
		j.list = j.list[len(j.list)-keepHealthJobs-1:]
	}
	return *job
}

// waker wakes the kind schedules.
type waker struct {
	mu  sync.Mutex
	chs []chan struct{}
}

// listen is a channel told each wake.
func (w *waker) listen() <-chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	ch := make(chan struct{}, 1)
	w.chs = append(w.chs, ch)
	return ch
}

func (w *waker) wake() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, ch := range w.chs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// kindPoll is how often a scheduled kind is looked at when nothing is due sooner: its
// settings, pause and sources may change in another mangal.
const kindPoll = 5 * time.Minute

// kindRetry is how long a kind waits after its run couldn't start.
const kindRetry = 15 * time.Minute

// kindSchedule runs a scheduled maintenance kind (source checks, source addresses) when
// core says it's due (JobKind.Next: never at startup, an interval after the last run),
// its missing ones as a job, and waits for it.
type kindSchedule struct {
	Kind  jobs.Kind
	Kinds func(context.Context) (core.JobKinds, error)
	// Wait waits for the job to end.
	Wait func(ctx context.Context, id int64) core.Job
	// Now is time.Now, After time.After (swapped in tests).
	Now   func() time.Time
	After func(time.Duration) <-chan time.Time
	Wake  <-chan struct{}
	Log   logger
}

func (s kindSchedule) loop(ctx context.Context) {
	for {
		wait := s.step(ctx)
		select {
		case <-ctx.Done():
			return
		case <-s.After(wait):
		case <-s.Wake:
		}
	}
}

// step runs the kind when it's due and says how long to wait.
func (s kindSchedule) step(ctx context.Context) time.Duration {
	kinds, err := s.Kinds(ctx)
	if err != nil {
		s.Log.Warnf("scheduled %s: %s", s.Kind, err)
		return kindRetry
	}
	k, err := kinds.Get(ctx, s.Kind)
	switch {
	case err != nil:
		s.Log.Warnf("scheduled %s: %s", s.Kind, err)
		return kindRetry
	case k.Next.IsZero():
		return kindPoll
	case k.Next.After(s.Now()):
		return min(kindPoll, k.Next.Sub(s.Now()))
	}
	job, err := kinds.Run(ctx, s.Kind, false)
	switch {
	case errors.Is(err, core.ErrNothingToDo):
		return kindPoll
	case err != nil:
		s.Log.Infof("scheduled %s: %s, tried again in %s", strings.ToLower(k.Title), err, kindRetry)
		return kindRetry
	}
	s.Log.Infof("%s (scheduled, job %d): %s", strings.ToLower(k.Title), job.ID, job.Title)
	j := s.Wait(ctx, job.ID)
	if j.Error != "" {
		s.Log.Warnf("%s (job %d): %s", strings.ToLower(k.Title), job.ID, j.Error)
	} else {
		s.Log.Infof("%s (job %d) done: %s", strings.ToLower(k.Title), job.ID, j.Message)
	}
	return 0
}

// waitJob waits for the job of m to end (or ctx), and is the job as it ended.
func waitJob(ctx context.Context, m *jobs.Manager, id int64) core.Job {
	for {
		changed := m.Changed()
		j, ok := m.Get(id)
		if !ok || !j.Status.Active() {
			return core.JobOf(j)
		}
		select {
		case <-ctx.Done():
			return core.JobOf(j)
		case <-changed:
		}
	}
}

// scheduledKinds are the kinds serve runs on their own besides the watch checks.
var scheduledKinds = []jobs.Kind{jobs.KindSourceCheck, jobs.KindSourceURLs}

// runKinds runs the schedules of the maintenance kinds until ctx ends.
func (a *API) runKinds(ctx context.Context) {
	m, err := a.jobs()
	if err != nil {
		return
	}
	for _, kind := range scheduledKinds {
		s := kindSchedule{Kind: kind, Kinds: a.kinds, Wait: func(ctx context.Context, id int64) core.Job { return waitJob(ctx, m, id) },
			Now: time.Now, After: time.After, Wake: a.wakeKinds.listen(), Log: a.Log}
		go s.loop(ctx)
	}
}

// checkGate is whether the scheduler checks the watches now (jobs.check.enabled, not
// paused) and which providers' (jobs.check.providers).
func (a *API) checkGate() (bool, func(string) bool) {
	if !config.Jobs.Check.Enabled.Fresh() || a.kindPaused(jobs.KindCheck) {
		return false, nil
	}
	return true, config.Jobs.Check.Providers.Fresh().On
}

// kindPaused is true when the kind's scheduling is paused.
func (a *API) kindPaused(kind jobs.Kind) bool {
	c, err := a.services(a.ctx)
	return err == nil && c.KindPaused(kind)
}

// kind is the request's kind with the server's kinds.
func (a *API) kind(ctx context.Context, name string) (jobs.Kind, core.JobKinds, error) {
	kind, err := core.ParseJobKind(name)
	if err != nil {
		return kind, nil, err
	}
	kinds, err := a.kinds(ctx)
	return kind, kinds, err
}

// ListJobKinds implements apiv1.StrictServerInterface.
func (a *API) ListJobKinds(ctx context.Context, _ apiv1.ListJobKindsRequestObject) (apiv1.ListJobKindsResponseObject, error) {
	kinds, err := a.kinds(ctx)
	if err != nil {
		return nil, err
	}
	list, err := kinds.List(ctx)
	if err != nil {
		return nil, err
	}
	return apiv1.ListJobKinds200JSONResponse(list), nil
}

// GetJobKind implements apiv1.StrictServerInterface.
func (a *API) GetJobKind(ctx context.Context, request apiv1.GetJobKindRequestObject) (apiv1.GetJobKindResponseObject, error) {
	kind, kinds, err := a.kind(ctx, request.Kind)
	if err != nil {
		return nil, err
	}
	k, err := kinds.Get(ctx, kind)
	if err != nil {
		return nil, err
	}
	return apiv1.GetJobKind200JSONResponse(k), nil
}

// RunJobKind implements apiv1.StrictServerInterface.
func (a *API) RunJobKind(ctx context.Context, request apiv1.RunJobKindRequestObject) (apiv1.RunJobKindResponseObject, error) {
	kind, kinds, err := a.kind(ctx, request.Kind)
	if err != nil {
		return nil, err
	}
	all := deref(deref(request.Body).All)
	job, err := kinds.Run(ctx, kind, all)
	if errors.Is(err, core.ErrRefused) {
		return nil, &apiError{status: http.StatusConflict, code: CodeRefused, err: err}
	}
	if err != nil {
		return nil, err
	}
	what := "missing"
	if all {
		what = "all"
	}
	a.Log.Infof("job %d (%s, %s, asked through the API): %s", job.ID, kind, what, job.Title)
	return apiv1.RunJobKind202JSONResponse(toAPIJob(job)), nil
}

func (a *API) pauseKind(ctx context.Context, name string, paused bool) (core.JobKind, error) {
	kind, kinds, err := a.kind(ctx, name)
	if err != nil {
		return core.JobKind{}, err
	}
	k, err := kinds.Pause(ctx, kind, paused)
	if err != nil {
		return k, err
	}
	what := "resumed"
	if paused {
		what = "paused"
	}
	a.Log.Infof("%s %s through the API", strings.ToLower(k.Title), what)
	return k, nil
}

// PauseJobKind implements apiv1.StrictServerInterface.
func (a *API) PauseJobKind(ctx context.Context, request apiv1.PauseJobKindRequestObject) (apiv1.PauseJobKindResponseObject, error) {
	k, err := a.pauseKind(ctx, request.Kind, true)
	if err != nil {
		return nil, err
	}
	return apiv1.PauseJobKind200JSONResponse(k), nil
}

// ResumeJobKind implements apiv1.StrictServerInterface.
func (a *API) ResumeJobKind(ctx context.Context, request apiv1.ResumeJobKindRequestObject) (apiv1.ResumeJobKindResponseObject, error) {
	k, err := a.pauseKind(ctx, request.Kind, false)
	if err != nil {
		return nil, err
	}
	return apiv1.ResumeJobKind200JSONResponse(k), nil
}

// SetJobKindSettings implements apiv1.StrictServerInterface: only an admin changes them.
func (a *API) SetJobKindSettings(ctx context.Context, request apiv1.SetJobKindSettingsRequestObject) (apiv1.SetJobKindSettingsResponseObject, error) {
	if err := needAdmin(ctx); err != nil {
		return nil, err
	}
	kind, kinds, err := a.kind(ctx, request.Kind)
	if err != nil {
		return nil, err
	}
	k, err := kinds.Set(ctx, kind, deref(request.Body))
	if err != nil {
		return nil, err
	}
	a.Log.Infof("%s settings changed through the API", strings.ToLower(k.Title))
	return apiv1.SetJobKindSettings200JSONResponse(k), nil
}

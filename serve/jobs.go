package serve

import (
	"context"
	"errors"
	"fmt"
	"github.com/luevano/mangal/v5/log"
	"net/http"
	"slices"
	"strings"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/selector"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/library"
	stringutil "github.com/luevano/mangal/v5/util/string"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// toAPIJob is the job as the API answers it.
func toAPIJob(j core.Job) apiv1.Job {
	job := apiv1.Job{Id: j.ID, Kind: j.Kind.String(), Title: j.Title, Providers: orEmpty(j.Providers), Status: j.Status.String(),
		Added: j.Added, Started: optTime(j.Started), Finished: optTime(j.Finished)}
	for _, f := range []struct {
		v string
		p **string
	}{{j.MangaID, &job.MangaId}, {j.Message, &job.Message}, {j.Note, &job.Note}, {j.Error, &job.Error}} {
		if f.v != "" {
			v := f.v
			*f.p = &v
		}
	}
	if j.Total != 0 {
		job.Done, job.Total = &j.Done, &j.Total
	}
	if j.Part != 0 {
		job.Part = &j.Part
	}
	if j.Ahead != 0 {
		job.Ahead, job.Behind = &j.Ahead, &j.Behind
	}
	if len(j.Results) != 0 {
		results := make([]apiv1.JobResult, 0, len(j.Results))
		for _, r := range j.Results {
			jr := apiv1.JobResult{Chapter: r.Chapter, Title: r.Title}
			for _, f := range []struct {
				v string
				p **string
			}{{r.Path, &jr.Path}, {r.Skipped, &jr.Skipped}, {r.Unavailable, &jr.Unavailable}, {r.Error, &jr.Error}} {
				if f.v != "" {
					v := f.v
					*f.p = &v
				}
			}
			results = append(results, jr)
		}
		job.Results = &results
	}
	return job
}

func (a *API) jobs() (*jobs.Manager, error) {
	if a.Jobs == nil {
		return nil, newAPIError(http.StatusServiceUnavailable, CodeUnavailable, "no jobs here")
	}
	return a.Jobs, nil
}

// jobService is the server's jobs as the core's service.
func (a *API) jobService() (core.Jobs, error) {
	m, err := a.jobs()
	if err != nil {
		return nil, err
	}
	return core.LocalJobs(m), nil
}

// ListJobs implements apiv1.StrictServerInterface: the active ones first, then the newest.
func (a *API) ListJobs(ctx context.Context, _ apiv1.ListJobsRequestObject) (apiv1.ListJobsResponseObject, error) {
	svc, err := a.jobService()
	if err != nil {
		return nil, err
	}
	list, err := svc.List(ctx)
	if err != nil {
		return nil, err
	}
	slices.SortStableFunc(list, func(x, y core.Job) int {
		if x.Status.Active() != y.Status.Active() {
			if x.Status.Active() {
				return -1
			}
			return 1
		}
		if x.Status.Active() {
			return int(x.ID - y.ID)
		}
		return int(y.ID - x.ID)
	})
	out := apiv1.ListJobs200JSONResponse{}
	for _, j := range list {
		out = append(out, toAPIJob(j))
	}
	return out, nil
}

// jobOrigin is the origin of the jobs the request starts: who it is ("api: user kid").
func jobOrigin(ctx context.Context) string {
	id, _ := identityOf(ctx)
	return "api: " + id.String()
}

// ownJob is the job when the request may change it: its own, or anyone's with manage.
func (a *API) ownJob(ctx context.Context, id int64) (core.Job, core.Jobs, error) {
	j, svc, err := a.job(ctx, id)
	if err != nil {
		return j, svc, err
	}
	if who, _ := identityOf(ctx); !who.has(ScopeManage) && j.Origin != jobOrigin(ctx) {
		return j, svc, newAPIError(http.StatusForbidden, CodeForbidden, "job %d isn't yours: someone else's jobs need the manage permission", id)
	}
	return j, svc, nil
}

func (a *API) job(ctx context.Context, id int64) (core.Job, core.Jobs, error) {
	svc, err := a.jobService()
	if err != nil {
		return core.Job{}, nil, err
	}
	j, err := svc.Get(ctx, id)
	if errors.Is(err, core.ErrNotFound) {
		return j, svc, &core.NotFoundError{What: fmt.Sprintf("no job %d (finished ones are kept a while)", id)}
	}
	return j, svc, err
}

// GetJob implements apiv1.StrictServerInterface.
func (a *API) GetJob(ctx context.Context, request apiv1.GetJobRequestObject) (apiv1.GetJobResponseObject, error) {
	j, _, err := a.job(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	return apiv1.GetJob200JSONResponse(toAPIJob(j)), nil
}

// CancelJob implements apiv1.StrictServerInterface.
func (a *API) CancelJob(ctx context.Context, request apiv1.CancelJobRequestObject) (apiv1.CancelJobResponseObject, error) {
	j, svc, err := a.ownJob(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	if ok, err := svc.Cancel(ctx, request.Id); err != nil {
		return nil, err
	} else if !ok {
		return nil, newAPIError(http.StatusConflict, CodeRefused, "job %d is %s", j.ID, j.Status)
	}
	log.Info("job %d (%s) cancelled through the API", j.ID, j.Title)
	if now, err := svc.Get(ctx, request.Id); err == nil {
		j = now
	}
	return apiv1.CancelJob202JSONResponse(toAPIJob(j)), nil
}

// RetryJob implements apiv1.StrictServerInterface.
func (a *API) RetryJob(ctx context.Context, request apiv1.RetryJobRequestObject) (apiv1.RetryJobResponseObject, error) {
	_, svc, err := a.ownJob(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	j, err := svc.Retry(ctx, request.Id)
	if err != nil {
		if errors.Is(err, jobs.ErrNothingToRetry) {
			return nil, &apiError{status: http.StatusConflict, code: CodeNothingToDo, err: err}
		}
		return nil, &apiError{status: http.StatusConflict, code: CodeRefused, err: err}
	}
	log.Info("job %d (%s) retried through the API", j.ID, j.Title)
	return apiv1.RetryJob202JSONResponse(toAPIJob(j)), nil
}

// RemoveJob implements apiv1.StrictServerInterface.
func (a *API) RemoveJob(ctx context.Context, request apiv1.RemoveJobRequestObject) (apiv1.RemoveJobResponseObject, error) {
	j, svc, err := a.ownJob(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	if ok, err := svc.Remove(ctx, request.Id); err != nil {
		return nil, err
	} else if !ok {
		return nil, newAPIError(http.StatusConflict, CodeRefused, "job %d is %s, cancel it first", j.ID, j.Status)
	}
	return apiv1.RemoveJob204Response{}, nil
}

// ClearJobQueue implements apiv1.StrictServerInterface.
func (a *API) ClearJobQueue(ctx context.Context, request apiv1.ClearJobQueueRequestObject) (apiv1.ClearJobQueueResponseObject, error) {
	svc, err := a.jobService()
	if err != nil {
		return nil, err
	}
	n, err := svc.ClearQueue(ctx, deref(deref(request.Body).Provider))
	if err != nil {
		return nil, err
	}
	if n > 0 {
		log.Info("%s cancelled through the API", stringutil.Quantify(n, "queued job", "queued jobs"))
	}
	return apiv1.ClearJobQueue200JSONResponse{Count: n}, nil
}

// StartDownload implements apiv1.StrictServerInterface: the manga is found (its id, or a
// search), its chapters picked with the selector, and the download queued in the
// provider's queue (the provider's lock: one download per provider in every mangal).
func (a *API) StartDownload(ctx context.Context, request apiv1.StartDownloadRequestObject) (apiv1.StartDownloadResponseObject, error) {
	m, err := a.jobs()
	if err != nil {
		return nil, err
	}
	b := deref(request.Body)
	if deref(b.MangaId) == "" && deref(b.Query) == "" {
		return nil, badRequest("manga_id or query is needed")
	}
	if b.Chapters == "" {
		return nil, badRequest("chapters: a chapter selector is needed (all, last, 12, 10-20, 30-)")
	}
	options := config.DownloadOptions()
	if f := deref(b.Format); f != "" {
		if who, _ := identityOf(ctx); !who.has(ScopeManage) {
			return nil, newAPIError(http.StatusForbidden, CodeForbidden, "picking the format needs the manage permission, the server's (%s) is used without it", options.Format)
		}
		format, err := libmangal.FormatString(strings.ToUpper(f))
		if err != nil {
			return nil, badRequest("format: %q isn't one of %s", f, strings.Join(libmangal.FormatStrings(), ", "))
		}
		options.Format = format
	}
	loader, err := a.loader(b.Provider)
	if err != nil {
		return nil, err
	}
	c, release, err := client.Hold(ctx, loader)
	if err != nil {
		return nil, unprocessable(err)
	}
	defer release()
	manga, err := selector.Find(ctx, client.Searcher(c), deref(b.MangaId), deref(b.Query), deref(b.Selector))
	if err != nil {
		return nil, unprocessable(err)
	}
	all, err := client.MangaChapters(ctx, c, manga)
	if err != nil {
		return nil, unprocessable(err)
	}
	chapters, err := selector.Chapters(b.Chapters, all)
	if err != nil {
		return nil, badRequest("chapters: %w", err)
	}
	if len(chapters) == 0 {
		return nil, badRequest("chapters: %q matches none", b.Chapters)
	}
	// a bound manga gets its metadata now, a search would replace it
	if bound, err := client.ApplyBinding(ctx, c, manga); err != nil {
		log.Warn("binding of %q: %s", manga, err)
	} else if bound {
		options.SearchMetadata = false
	}
	title := fmt.Sprintf("%s: %s", manga.Info().Title, chaptersTitle(chapters))
	job := m.Add(jobs.DownloadSpec(c, manga, chapters, options, title, jobOrigin(ctx)))
	go a.logDownload(m, job.ID)
	log.Info("download %d (asked through the API): %s from %s", job.ID, title, c.Info().ID)
	return apiv1.StartDownload202JSONResponse(toAPIJob(core.JobOf(job))), nil
}

// chaptersTitle is "chapter 3" or "chapters 1-5 (4)".
func chaptersTitle(chapters []mangadata.Chapter) string {
	first, last := chapters[0].Info().Number, chapters[len(chapters)-1].Info().Number
	if len(chapters) == 1 {
		return "chapter " + stringutil.FormatFloat32(first)
	}
	return fmt.Sprintf("chapters %s-%s (%d)", stringutil.FormatFloat32(first), stringutil.FormatFloat32(last), len(chapters))
}

// ScanLibrary implements apiv1.StrictServerInterface: the scan runs as a job.
func (a *API) ScanLibrary(ctx context.Context, request apiv1.ScanLibraryRequestObject) (apiv1.ScanLibraryResponseObject, error) {
	m, err := a.jobs()
	if err != nil {
		return nil, err
	}
	c, err := a.services(ctx)
	if err != nil {
		return nil, err
	}
	b := deref(request.Body)
	opts := core.ScanOptions{Check: deref(b.Check), Statuses: deref(b.Statuses)}
	job := m.Add(jobs.Spec{Kind: jobs.KindScan, Title: "library scan", Origin: "api", Run: func(ctx context.Context, p *jobs.Progress) error {
		root, err := c.Library.Root(ctx)
		if err != nil {
			return err
		}
		p.Running("scanning " + root)
		ctx = core.WithProgress(ctx, func(pr library.Progress) { p.Message(pr.String()) })
		rep, err := c.Library.Scan(ctx, opts)
		if err != nil {
			return err
		}
		msg := core.ScanSummary(rep)
		p.Message(msg)
		log.Info("library scan (asked through the API): %s", msg)
		return nil
	}})
	return apiv1.ScanLibrary202JSONResponse(toAPIJob(core.JobOf(job))), nil
}

// LookupSourceURLs implements apiv1.StrictServerInterface.
func (a *API) LookupSourceURLs(ctx context.Context, request apiv1.LookupSourceURLsRequestObject) (apiv1.LookupSourceURLsResponseObject, error) {
	m, err := a.jobs()
	if err != nil {
		return nil, err
	}
	c, err := a.services(ctx)
	if err != nil {
		return nil, err
	}
	opts := core.SourceURLsOptions{All: deref(deref(request.Body).All)}
	job := m.Add(jobs.Spec{Kind: jobs.KindSourceURLs, Title: "source urls", Origin: "api", Run: func(ctx context.Context, p *jobs.Progress) error {
		p.Running("looking up the source addresses")
		ctx = core.WithProgress(ctx, func(pr library.Progress) {
			p.Step(pr.Done, pr.Total)
			p.Message(pr.String())
		})
		rep, err := c.Library.LookupSourceURLs(ctx, opts)
		msg := core.SourceURLsSummary(rep)
		p.Message(msg)
		log.Info("source urls (asked through the API): %s", msg)
		return err
	}})
	return apiv1.LookupSourceURLs202JSONResponse(toAPIJob(core.JobOf(job))), nil
}

// logDownload logs a download job's failed chapters and its outcome once it ends.
func (a *API) logDownload(m *jobs.Manager, id int64) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	waitJob(ctx, m, id)
	j, ok := m.Get(id)
	if !ok || j.Status.Active() {
		return
	}
	_, done, failed := j.Results.GetEach()
	for _, r := range failed {
		log.Warn("download %d: chapter %s: %s", id, stringutil.FormatFloat32(r.Chapter.Info().Number), r.Err)
	}
	line := fmt.Sprintf("download %d (%s) %s: %d downloaded, %d failed, %d unavailable", id, j.Title, j.Status,
		len(done), len(failed), len(j.Results.Unavailable()))
	if j.Err != nil {
		line += ": " + j.Err.Error()
	}
	if len(failed) != 0 || j.Err != nil {
		log.Warn("%s", line)
		return
	}
	log.Info("%s", line)
}

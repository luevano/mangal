package serve

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/jobs"
	"github.com/luevano/mangal/v5/client/selector"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/library"
	stringutil "github.com/luevano/mangal/v5/util/string"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// toAPIJob is the job as the API answers it.
func toAPIJob(j jobs.Job) apiv1.Job {
	job := apiv1.Job{Id: j.ID, Kind: j.Kind.String(), Title: j.Title, Providers: orEmpty(j.Providers), Status: j.Status.String(),
		Added: j.Added, Started: optTime(j.Started), Finished: optTime(j.Finished)}
	if j.MangaID != "" {
		job.MangaId = &j.MangaID
	}
	if j.Message != "" {
		job.Message = &j.Message
	}
	if j.Note != "" {
		job.Note = &j.Note
	}
	if j.Total != 0 {
		job.Done, job.Total = &j.Done, &j.Total
	}
	if j.Part != 0 {
		job.Part = &j.Part
	}
	if j.Err != nil {
		e := j.Err.Error()
		job.Error = &e
	}
	if j.Ahead != 0 {
		job.Ahead, job.Behind = &j.Ahead, &j.Behind
	}
	if len(j.Results) != 0 {
		results := make([]apiv1.JobResult, 0, len(j.Results))
		for _, r := range j.Results {
			info := r.Chapter.Info()
			jr := apiv1.JobResult{Chapter: info.Number, Title: info.Title}
			if r.Down != nil {
				p := r.Down.Path()
				jr.Path = &p
				if r.Down.Skipped != "" {
					jr.Skipped = &r.Down.Skipped
				}
			}
			if r.Unavailable != "" {
				jr.Unavailable = &r.Unavailable
			}
			if r.Err != nil {
				e := r.Err.Error()
				jr.Error = &e
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

// ListJobs implements apiv1.StrictServerInterface: the active ones first, then the newest.
func (a *API) ListJobs(context.Context, apiv1.ListJobsRequestObject) (apiv1.ListJobsResponseObject, error) {
	m, err := a.jobs()
	if err != nil {
		return nil, err
	}
	list := m.Jobs()
	slices.SortStableFunc(list, func(x, y jobs.Job) int {
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

func (a *API) job(id int64) (jobs.Job, error) {
	m, err := a.jobs()
	if err != nil {
		return jobs.Job{}, err
	}
	j, ok := m.Get(id)
	if !ok {
		return j, &core.NotFoundError{What: fmt.Sprintf("no job %d (finished ones are kept a while)", id)}
	}
	return j, nil
}

// GetJob implements apiv1.StrictServerInterface.
func (a *API) GetJob(_ context.Context, request apiv1.GetJobRequestObject) (apiv1.GetJobResponseObject, error) {
	j, err := a.job(request.Id)
	if err != nil {
		return nil, err
	}
	return apiv1.GetJob200JSONResponse(toAPIJob(j)), nil
}

// CancelJob implements apiv1.StrictServerInterface.
func (a *API) CancelJob(_ context.Context, request apiv1.CancelJobRequestObject) (apiv1.CancelJobResponseObject, error) {
	j, err := a.job(request.Id)
	if err != nil {
		return nil, err
	}
	if !a.Jobs.Cancel(request.Id) {
		return nil, newAPIError(http.StatusConflict, CodeRefused, "job %d is %s", j.ID, j.Status)
	}
	a.Log.Infof("job %d (%s) cancelled through the API", j.ID, j.Title)
	j, _ = a.Jobs.Get(request.Id)
	return apiv1.CancelJob202JSONResponse(toAPIJob(j)), nil
}

// RetryJob implements apiv1.StrictServerInterface.
func (a *API) RetryJob(_ context.Context, request apiv1.RetryJobRequestObject) (apiv1.RetryJobResponseObject, error) {
	if _, err := a.job(request.Id); err != nil {
		return nil, err
	}
	j, err := a.Jobs.Retry(request.Id)
	if err != nil {
		if errors.Is(err, jobs.ErrNothingToRetry) {
			return nil, &apiError{status: http.StatusConflict, code: CodeNothingToDo, err: err}
		}
		return nil, &apiError{status: http.StatusConflict, code: CodeRefused, err: err}
	}
	a.Log.Infof("job %d (%s) retried through the API", j.ID, j.Title)
	return apiv1.RetryJob202JSONResponse(toAPIJob(j)), nil
}

// RemoveJob implements apiv1.StrictServerInterface.
func (a *API) RemoveJob(_ context.Context, request apiv1.RemoveJobRequestObject) (apiv1.RemoveJobResponseObject, error) {
	j, err := a.job(request.Id)
	if err != nil {
		return nil, err
	}
	if !a.Jobs.Remove(request.Id) {
		return nil, newAPIError(http.StatusConflict, CodeRefused, "job %d is %s, cancel it first", j.ID, j.Status)
	}
	return apiv1.RemoveJob204Response{}, nil
}

// ClearJobQueue implements apiv1.StrictServerInterface.
func (a *API) ClearJobQueue(_ context.Context, request apiv1.ClearJobQueueRequestObject) (apiv1.ClearJobQueueResponseObject, error) {
	m, err := a.jobs()
	if err != nil {
		return nil, err
	}
	n := m.ClearQueue(deref(deref(request.Body).Provider))
	if n > 0 {
		a.Log.Infof("%s cancelled through the API", stringutil.Quantify(n, "queued job", "queued jobs"))
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
	c, err := client.Acquire(ctx, loader)
	if err != nil {
		return nil, unprocessable(err)
	}
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
	// a bound manga gets its metadata now, a search would replace it
	if bound, err := client.ApplyBinding(ctx, c, manga); err != nil {
		a.Log.Warnf("binding of %q: %s", manga, err)
	} else if bound {
		options.SearchMetadata = false
	}
	title := fmt.Sprintf("%s: %s", manga.Info().Title, chaptersTitle(chapters))
	job := m.Add(jobs.DownloadSpec(c, manga, chapters, options, title, "api"))
	a.Log.Infof("download %d (asked through the API): %s from %s", job.ID, title, c.Info().ID)
	return apiv1.StartDownload202JSONResponse(toAPIJob(job)), nil
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
		p.Running("scanning " + c.Library.Root())
		ctx = core.WithProgress(ctx, func(pr library.Progress) { p.Message(pr.String()) })
		rep, err := c.Library.Scan(ctx, opts)
		if err != nil {
			return err
		}
		msg := scanSummary(rep)
		p.Message(msg)
		a.Log.Infof("library scan (asked through the API): %s", msg)
		return nil
	}})
	return apiv1.ScanLibrary202JSONResponse(toAPIJob(job)), nil
}

// scanSummary is what a scan found, in a line.
func scanSummary(rep core.ScanReport) string {
	r := rep.Result
	parts := []string{fmt.Sprintf("%d chapters added, %d moved, %d removed, %d new folders", r.Added, r.Moved, r.Removed, r.New)}
	if len(r.Missing) != 0 {
		parts = append(parts, fmt.Sprintf("%d missing", len(r.Missing)))
	}
	if len(rep.Duplicates) != 0 {
		parts = append(parts, fmt.Sprintf("%d duplicates", len(rep.Duplicates)))
	}
	return strings.Join(parts, ", ")
}

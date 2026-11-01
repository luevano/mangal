package remote

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// Jobs are the server's jobs (core.Jobs). MangaStatus, ChapterStatus and Active answer
// from a cache that List, the calls and Subscribe's events keep; with a Subscribe
// running, Get does too.
type Jobs struct {
	c *Client

	mu    sync.Mutex
	cache map[int64]core.Job
	// live is how many Subscribes feed the cache
	live int
	// origins are the Origin of the downloads this client started (the API has the
	// server's: who asked)
	origins map[int64]string
}

var _ core.Jobs = (*Jobs)(nil)

// Jobs are the server's jobs, one cache per client.
func (c *Client) Jobs() *Jobs {
	c.jobsOnce.Do(func() { c.jobs = &Jobs{c: c, cache: map[int64]core.Job{}, origins: map[int64]string{}} })
	return c.jobs
}

// jobOf is the API's job as core's.
func jobOf(j apiv1.Job) core.Job {
	out := core.Job{ID: j.Id, Title: j.Title, Providers: j.Providers, MangaID: deref(j.MangaId), Message: deref(j.Message),
		Note: deref(j.Note), Done: deref(j.Done), Total: deref(j.Total), Part: deref(j.Part), Error: deref(j.Error),
		Added: j.Added, Started: deref(j.Started), Finished: deref(j.Finished), Ahead: deref(j.Ahead), Behind: deref(j.Behind), Read: deref(j.Read)}
	// an unknown kind or status (a newer server's) stays the zero one
	_ = out.Kind.UnmarshalText([]byte(j.Kind))
	_ = out.Status.UnmarshalText([]byte(j.Status))
	for _, r := range deref(j.Results) {
		out.Results = append(out.Results, core.JobResult{Chapter: r.Chapter, Title: r.Title, Path: deref(r.Path), Status: deref(r.Status),
			Skipped: deref(r.Skipped), Unavailable: deref(r.Unavailable), Error: deref(r.Error)})
	}
	return out
}

// keep caches the job, unless it's an older state of one over (an answer that came
// after the job's finished event); a retry (requeued) makes it active again.
func (j *Jobs) keep(job core.Job, requeued bool) core.Job {
	j.mu.Lock()
	defer j.mu.Unlock()
	job.Origin = j.origins[job.ID]
	if old, ok := j.cache[job.ID]; ok && !requeued && !old.Status.Active() && job.Status.Active() {
		return old
	}
	j.cache[job.ID] = job
	return job
}

func (j *Jobs) drop(id int64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	delete(j.cache, id)
	delete(j.origins, id)
}

// job is the job with the id on the server.
func (c *Client) job(ctx context.Context, id int64) (core.Job, error) {
	r, err := c.API.GetJobWithResponse(ctx, id)
	if err != nil {
		return core.Job{}, fmt.Errorf("mangal serve: %w", err)
	}
	job, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	if err != nil {
		return core.Job{}, err
	}
	return c.Jobs().keep(jobOf(job), false), nil
}

// List is the server's jobs in the order they were added.
func (j *Jobs) List(ctx context.Context) ([]core.Job, error) {
	r, err := j.c.API.ListJobsWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	list, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	if err != nil {
		return nil, err
	}
	out := make([]core.Job, len(list))
	cache := make(map[int64]core.Job, len(list))
	j.mu.Lock()
	for i, job := range list {
		out[i] = jobOf(job)
		out[i].Origin = j.origins[out[i].ID]
		cache[out[i].ID] = out[i]
	}
	j.cache = cache
	j.mu.Unlock()
	slices.SortFunc(out, func(a, b core.Job) int { return cmp.Compare(a.ID, b.ID) })
	return out, nil
}

func (j *Jobs) Get(ctx context.Context, id int64) (core.Job, error) {
	j.mu.Lock()
	job, ok := j.cache[id]
	live := j.live > 0
	j.mu.Unlock()
	if ok && live {
		return job, nil
	}
	return j.c.job(ctx, id)
}

// refused is the server refusing what the job's state doesn't allow (409 refused).
func refused(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == http.StatusConflict && e.Code == "refused"
}

func (j *Jobs) Cancel(ctx context.Context, id int64) (bool, error) {
	r, err := j.c.API.CancelJobWithResponse(ctx, id)
	if err != nil {
		return false, fmt.Errorf("mangal serve: %w", err)
	}
	job, err := answer(r.StatusCode(), r.Body, r.JSON202, r.JSONDefault)
	if refused(err) || errors.Is(err, core.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	j.keep(jobOf(job), false)
	return true, nil
}

func (j *Jobs) Retry(ctx context.Context, id int64) (core.Job, error) {
	r, err := j.c.API.RetryJobWithResponse(ctx, id)
	if err != nil {
		return core.Job{}, fmt.Errorf("mangal serve: %w", err)
	}
	job, err := answer(r.StatusCode(), r.Body, r.JSON202, r.JSONDefault)
	if err != nil {
		return core.Job{}, err
	}
	return j.keep(jobOf(job), true), nil
}

func (j *Jobs) Remove(ctx context.Context, id int64) (bool, error) {
	r, err := j.c.API.RemoveJobWithResponse(ctx, id)
	if err != nil {
		return false, fmt.Errorf("mangal serve: %w", err)
	}
	if r.StatusCode() >= 300 {
		err := errorOf(r.StatusCode(), r.Body, r.JSONDefault)
		if refused(err) || errors.Is(err, core.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	j.drop(id)
	return true, nil
}

func (j *Jobs) ClearQueue(ctx context.Context, provider string) (int, error) {
	r, err := j.c.API.ClearJobQueueWithResponse(ctx, apiv1.ClearJobsRequest{Provider: opt(provider)})
	if err != nil {
		return 0, fmt.Errorf("mangal serve: %w", err)
	}
	n, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	return n.Count, err
}

// CancelAll cancels the server's active jobs (the token's, everyone's with manage); on a
// server before API 1.1 one by one, a job added meanwhile goes on.
func (j *Jobs) CancelAll(ctx context.Context) (int, error) {
	if j.c.needsAPI(ctx, 1, "") == nil {
		r, err := j.c.API.CancelAllJobsWithResponse(ctx)
		if err != nil {
			return 0, fmt.Errorf("mangal serve: %w", err)
		}
		n, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
		if err == nil {
			// the cancelled ones' events follow, a List refreshes the cache meanwhile
			_, _ = j.List(ctx)
		}
		return n.Count, err
	}
	list, err := j.List(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, job := range list {
		if !job.Status.Active() {
			continue
		}
		ok, err := j.Cancel(ctx, job.ID)
		if err != nil {
			return n, err
		}
		if ok {
			n++
		}
	}
	return n, nil
}

// Download queues the download on the server, with its providers and download settings:
// Have is only read for the manga's id and the chapters. Read isn't on the API: a
// remote frontend reads from the server's files or pages (Reader).
func (j *Jobs) Download(ctx context.Context, req core.DownloadReq) (core.Job, error) {
	if req.Read {
		return core.Job{}, unsupported("downloading for reading")
	}
	if req.Have != nil {
		req.MangaID = req.Have.Manga.Info().ID
		if req.Chapters == "" && len(req.ChapterRefs) == 0 {
			for _, ch := range req.Have.Chapters {
				req.ChapterRefs = append(req.ChapterRefs, core.ChapterRefOf(ch))
			}
		}
	}
	body := apiv1.DownloadRequest{Provider: req.Provider, MangaId: opt(req.MangaID), Query: opt(req.Query), Selector: opt(req.Selector),
		Chapters: opt(req.Chapters), Format: opt(req.Format)}
	if len(req.ChapterRefs) != 0 {
		if err := j.c.needsAPI(ctx, 1, "downloading chosen chapters"); err != nil {
			return core.Job{}, err
		}
		refs := make([]apiv1.ChapterRef, len(req.ChapterRefs))
		for i, r := range req.ChapterRefs {
			refs[i] = apiv1.ChapterRef{Number: r.Number, Title: opt(r.Title), Url: opt(r.URL)}
		}
		body.ChapterRefs = &refs
	}
	r, err := j.c.API.StartDownloadWithResponse(ctx, body)
	if err != nil {
		return core.Job{}, fmt.Errorf("mangal serve: %w", err)
	}
	job, err := answer(r.StatusCode(), r.Body, r.JSON202, r.JSONDefault)
	if err != nil {
		return core.Job{}, err
	}
	if req.Origin != "" {
		j.mu.Lock()
		j.origins[job.Id] = req.Origin
		j.mu.Unlock()
	}
	return j.keep(jobOf(job), true), nil
}

// Active are the cached active jobs, in the order they were added.
func (j *Jobs) Active() []core.Job {
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []core.Job
	for _, job := range j.cache {
		if job.Status.Active() {
			out = append(out, job)
		}
	}
	slices.SortFunc(out, func(a, b core.Job) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

func (j *Jobs) MangaStatus(provider, mangaID string) (jobs.Status, bool) {
	var best jobs.Status
	found := false
	for _, job := range j.Active() {
		if job.Kind == jobs.KindDownload && job.MangaID == mangaID && slices.Contains(job.Providers, provider) && (!found || job.Status > best) {
			best, found = job.Status, true
		}
	}
	return best, found
}

// ChapterStatus matches the chapter by number and title: the API's results have no address.
func (j *Jobs) ChapterStatus(provider, mangaID string, chapter core.ChapterRef) (jobs.Status, bool) {
	for _, job := range j.Active() {
		if job.Kind != jobs.KindDownload || job.MangaID != mangaID || !slices.Contains(job.Providers, provider) {
			continue
		}
		for _, r := range job.Results {
			if r.Chapter == chapter.Number && (chapter.Title == "" || r.Title == chapter.Title) && r.ToDownload() {
				return job.Status, true
			}
		}
	}
	return 0, false
}

// Subscribe lists the jobs (the cache) then sends the server's job events after the
// one with Seq after (0: from now, not the ones the server keeps). A JobChanged is
// dropped while the reader is behind.
func (j *Jobs) Subscribe(ctx context.Context, after uint64) (<-chan core.JobEvent, error) {
	if after == 0 {
		st, err := j.c.Status(ctx)
		if err != nil {
			return nil, err
		}
		after = deref(st.EventSeq)
	}
	if _, err := j.List(ctx); err != nil {
		return nil, err
	}
	j.mu.Lock()
	j.live++
	j.mu.Unlock()
	ch := make(chan core.JobEvent, 16)
	go func() {
		defer close(ch)
		defer func() {
			j.mu.Lock()
			j.live--
			j.mu.Unlock()
		}()
		err := j.c.Events(ctx, []string{"job"}, after, func(e Event) {
			var api apiv1.Job
			if json.Unmarshal(e.Data, &api) != nil {
				return
			}
			kind := core.JobEventKind(e.Kind)
			ev := core.JobEvent{Seq: e.Seq, Kind: kind, Job: j.keep(jobOf(api), kind == core.JobAdded)}
			if ev.Kind == core.JobChanged {
				// the job isn't sent: the local jobs' changed events have none
				ev.Job = core.Job{}
				select {
				case ch <- ev:
				default:
				}
				return
			}
			select {
			case ch <- ev:
			case <-ctx.Done():
			}
		})
		if err != nil {
			log.Warn("remote: the server's job events: %s", err)
		}
	}()
	return ch, nil
}

// needsAPI is an error when the server's API is older than 1.minor: what isn't there.
func (c *Client) needsAPI(ctx context.Context, minor int, what string) error {
	st, err := c.status(ctx)
	if err != nil {
		return err
	}
	_, rest, _ := strings.Cut(st.ApiVersion, ".")
	have, _, _ := strings.Cut(rest, ".")
	if n, err := strconv.Atoi(have); err == nil && n >= minor {
		return nil
	}
	return fmt.Errorf("%s needs API 1.%d, the server (mangal %s) has %s: update it", what, minor, deref(st.Version), st.ApiVersion)
}

package core

import (
	"context"
	"fmt"
	"time"

	"github.com/luevano/mangal/v5/core/jobs"
)

// Jobs are the background jobs (downloads, watch checks, scans) of whoever runs them:
// the TUI's own, mangal serve's (LocalJobs over their jobs.Manager).
type Jobs interface {
	// List is every listed job, in the order they were added.
	List(ctx context.Context) ([]Job, error)
	// Get is the job with the id, a *NotFoundError when it isn't listed (any more).
	Get(ctx context.Context, id int64) (Job, error)
	// Cancel stops an active job, false if it's over or not listed.
	Cancel(ctx context.Context, id int64) (bool, error)
	// Retry queues a download again with its failed chapters (and those a cancel left).
	Retry(ctx context.Context, id int64) (Job, error)
	// Remove drops a finished job, false if it's active or not listed.
	Remove(ctx context.Context, id int64) (bool, error)
	// ClearQueue drops the queued jobs of the provider ("" for every one), how many.
	ClearQueue(ctx context.Context, provider string) (int, error)
	// CancelAll stops every active job, how many.
	CancelAll(ctx context.Context) (int, error)
}

// Job is a job as it was when asked for.
type Job struct {
	ID    int64     `json:"id"`
	Kind  jobs.Kind `json:"kind"`
	Title string    `json:"title"`
	// Providers are the ones it downloads from: it waits in each one's queue.
	Providers []string `json:"providers"`
	// MangaID is the provider's manga of a download.
	MangaID string `json:"manga_id,omitempty"`
	// Origin is the window key of the screen that started it.
	Origin string      `json:"origin,omitempty"`
	Read   bool        `json:"read,omitempty"`
	Status jobs.Status `json:"status"`
	// Message is what it does now (or did last, once over).
	Message string `json:"message,omitempty"`
	// Note is the provider's latest status (a mirror switch, a rate limit), NoteAt when.
	Note   string    `json:"note,omitempty"`
	NoteAt time.Time `json:"note_at,omitzero"`
	// Done of Total steps (a download's chapters), Part the share of the current one.
	Done  int     `json:"done,omitempty"`
	Total int     `json:"total,omitempty"`
	Part  float64 `json:"part,omitempty"`
	Error string  `json:"error,omitempty"`
	// Results are a download's chapters.
	Results  []JobResult `json:"results,omitempty"`
	Added    time.Time   `json:"added"`
	Started  time.Time   `json:"started,omitzero"`
	Finished time.Time   `json:"finished,omitzero"`
	// Ahead is how many jobs go before it in its providers' queues while queued, Behind
	// the title of the first of them.
	Ahead  int    `json:"ahead,omitempty"`
	Behind string `json:"behind,omitempty"`
}

// JobResult is a chapter of a download job.
type JobResult struct {
	Chapter float32 `json:"chapter"`
	Title   string  `json:"title"`
	// Path is the downloaded file, Skipped why it wasn't written (it exists).
	Path    string `json:"path,omitempty"`
	Skipped string `json:"skipped,omitempty"`
	// Unavailable is why it wasn't tried (the provider can't download it).
	Unavailable string `json:"unavailable,omitempty"`
	Error       string `json:"error,omitempty"`
}

// Failed is true when it failed to download.
func (r JobResult) Failed() bool { return r.Error != "" }

// ToDownload is true while it's yet to be downloaded.
func (r JobResult) ToDownload() bool { return !r.Failed() && r.Path == "" && r.Unavailable == "" }

// Retryable is true for a finished download with chapters failed or left.
func (j Job) Retryable() bool {
	if j.Status.Active() || j.Kind != jobs.KindDownload {
		return false
	}
	for _, r := range j.Results {
		if r.Failed() || r.ToDownload() {
			return true
		}
	}
	return false
}

// JobOf is the job's data.
func JobOf(j jobs.Job) Job {
	out := Job{ID: j.ID, Kind: j.Kind, Title: j.Title, Providers: j.Providers, MangaID: j.MangaID, Origin: j.Origin, Read: j.Read,
		Status: j.Status, Message: j.Message, Note: j.Note, NoteAt: j.NoteAt, Done: j.Done, Total: j.Total, Part: j.Part,
		Added: j.Added, Started: j.Started, Finished: j.Finished, Ahead: j.Ahead, Behind: j.Behind}
	if out.Providers == nil {
		out.Providers = []string{}
	}
	if j.Err != nil {
		out.Error = j.Err.Error()
	}
	for _, r := range j.Results {
		info := r.Chapter.Info()
		jr := JobResult{Chapter: info.Number, Title: info.Title, Unavailable: r.Unavailable}
		if r.Down != nil {
			jr.Path, jr.Skipped = r.Down.Path(), r.Down.Skipped
		}
		if r.Err != nil {
			jr.Error = r.Err.Error()
		}
		out.Results = append(out.Results, jr)
	}
	return out
}

// LocalJobs are the jobs of the process's manager m.
func LocalJobs(m *jobs.Manager) Jobs { return localJobs{m} }

type localJobs struct {
	m *jobs.Manager
}

func (l localJobs) List(context.Context) ([]Job, error) {
	list := l.m.Jobs()
	out := make([]Job, len(list))
	for i, j := range list {
		out[i] = JobOf(j)
	}
	return out, nil
}

func (l localJobs) Get(_ context.Context, id int64) (Job, error) {
	j, ok := l.m.Get(id)
	if !ok {
		return Job{}, &NotFoundError{What: fmt.Sprintf("job %d isn't listed", id)}
	}
	return JobOf(j), nil
}

func (l localJobs) Cancel(_ context.Context, id int64) (bool, error) { return l.m.Cancel(id), nil }

func (l localJobs) Retry(_ context.Context, id int64) (Job, error) {
	j, err := l.m.Retry(id)
	if err != nil {
		return Job{}, err
	}
	return JobOf(j), nil
}

func (l localJobs) Remove(_ context.Context, id int64) (bool, error) { return l.m.Remove(id), nil }

func (l localJobs) ClearQueue(_ context.Context, provider string) (int, error) {
	return l.m.ClearQueue(provider), nil
}

func (l localJobs) CancelAll(context.Context) (int, error) { return l.m.CancelAll(), nil }

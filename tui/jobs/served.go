package jobs

import (
	"context"
	"slices"
	"sync"

	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/log"
)

// served are the jobs of a TUI on this machine with a mangal serve of its data: the downloads
// are serve's (they go on when the TUI quits), a download for reading stays here (it opens a
// file here), as does what's the manager's. Serve's ids are negative: the manager's two
// counters would clash, and its own ids stay what its callers got from it.
type served struct {
	local core.Jobs
	serve core.Jobs
}

var _ core.Jobs = served{}

func fromServe(j core.Job) core.Job {
	j.ID = -j.ID
	return j
}

func (s served) List(ctx context.Context) ([]core.Job, error) {
	list, err := s.local.List(ctx)
	if err != nil {
		return nil, err
	}
	theirs, err := s.serve.List(ctx)
	for _, j := range theirs {
		list = append(list, fromServe(j))
	}
	slices.SortStableFunc(list, func(a, b core.Job) int { return a.Added.Compare(b.Added) })
	return list, err
}

func (s served) Get(ctx context.Context, id int64) (core.Job, error) {
	if id >= 0 {
		return s.local.Get(ctx, id)
	}
	j, err := s.serve.Get(ctx, -id)
	return fromServe(j), err
}

func (s served) Cancel(ctx context.Context, id int64) (bool, error) {
	if id >= 0 {
		return s.local.Cancel(ctx, id)
	}
	return s.serve.Cancel(ctx, -id)
}

func (s served) Retry(ctx context.Context, id int64) (core.Job, error) {
	if id >= 0 {
		return s.local.Retry(ctx, id)
	}
	j, err := s.serve.Retry(ctx, -id)
	return fromServe(j), err
}

func (s served) Remove(ctx context.Context, id int64) (bool, error) {
	if id >= 0 {
		return s.local.Remove(ctx, id)
	}
	return s.serve.Remove(ctx, -id)
}

func (s served) ClearQueue(ctx context.Context, provider string) (int, error) {
	n, err := s.local.ClearQueue(ctx, provider)
	if err != nil {
		return n, err
	}
	m, err := s.serve.ClearQueue(ctx, provider)
	return n + m, err
}

func (s served) CancelAll(ctx context.Context) (int, error) {
	n, err := s.local.CancelAll(ctx)
	if err != nil {
		return n, err
	}
	m, err := s.serve.CancelAll(ctx)
	return n + m, err
}

func (s served) Download(ctx context.Context, req core.DownloadReq) (core.Job, error) {
	if req.Read {
		return s.local.Download(ctx, req)
	}
	j, err := s.serve.Download(ctx, req)
	return fromServe(j), err
}

func (s served) MangaStatus(provider, mangaID string) (jobs.Status, bool) {
	a, oka := s.local.MangaStatus(provider, mangaID)
	b, okb := s.serve.MangaStatus(provider, mangaID)
	return max(a, b), oka || okb
}

func (s served) ChapterStatus(provider, mangaID string, chapter core.ChapterRef) (jobs.Status, bool) {
	a, oka := s.local.ChapterStatus(provider, mangaID, chapter)
	b, okb := s.serve.ChapterStatus(provider, mangaID, chapter)
	return max(a, b), oka || okb
}

// Subscribe merges both. Without serve's events (it stopped) the local jobs' go on.
func (s served) Subscribe(ctx context.Context, after uint64) (<-chan core.JobEvent, error) {
	local, err := s.local.Subscribe(ctx, after)
	if err != nil {
		return nil, err
	}
	out := make(chan core.JobEvent, 16)
	var wg sync.WaitGroup
	forward := func(ch <-chan core.JobEvent, of func(core.JobEvent) core.JobEvent) {
		defer wg.Done()
		for e := range ch {
			select {
			case out <- of(e):
			case <-ctx.Done():
				return
			}
		}
	}
	wg.Add(1)
	go forward(local, func(e core.JobEvent) core.JobEvent { return e })
	theirs, err := s.serve.Subscribe(ctx, after)
	if err != nil {
		log.Warn("tui: mangal serve's job events: %s", err)
	} else {
		wg.Add(1)
		go forward(theirs, func(e core.JobEvent) core.JobEvent {
			e.Job = fromServe(e.Job)
			return e
		})
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	return out, nil
}

// Active is serve's jobs as last heard, drawn on every view: no request.
func (s served) Active() []core.Job {
	list, err := s.local.List(context.Background())
	if err != nil {
		log.Warn("tui: list the jobs: %s", err)
	}
	list = slices.DeleteFunc(list, func(j core.Job) bool { return !j.Status.Active() })
	if c, ok := s.serve.(cached); ok {
		for _, j := range c.Active() {
			list = append(list, fromServe(j))
		}
	}
	slices.SortStableFunc(list, func(a, b core.Job) int { return a.Added.Compare(b.Added) })
	return list
}

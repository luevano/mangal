package watch

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Remote is the Manager of a running `mangal serve`, through its API.
//
// Check runs in the server: OnEvent and OnRun aren't called (Subscribe has the progress), the Report has what the
// server recorded (the Result's Gaps, Unavailable and Failed aren't). A cancelled ctx
// cancels the check in the server.
type Remote struct {
	// Address is the API's base URL, e.g. http://127.0.0.1:6977.
	Address string
	// Token is sent as a bearer token when not empty (serve.token).
	Token string
	// Client defaults to a client with a timeout.
	Client *http.Client
	// PollInterval is how often Check asks whether its run finished, 1s if 0.
	PollInterval time.Duration
}

var _ Manager = (*Remote)(nil)

// NewRemote is the Manager of the server at the address.
func NewRemote(address, token string) *Remote {
	return &Remote{
		Address: strings.TrimSuffix(address, "/"),
		Token:   token,
		Client:  &http.Client{Timeout: 5 * time.Minute},
	}
}

// do sends the request with body as JSON (if not nil) and decodes the answer into out (if
// not nil). Errors answered by the server are *APIError.
func (r *Remote) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.Address+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if r.Token != "" {
		req.Header.Set("Authorization", "Bearer "+r.Token)
	}
	client := r.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("mangal serve: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var e ErrorResponse
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		if json.Unmarshal(b, &e) != nil || e.Error == "" {
			e.Error = strings.TrimSpace(string(b))
			if e.Error == "" {
				e.Error = resp.Status
			}
		}
		return &APIError{Status: resp.StatusCode, Message: e.Error, Code: e.Code}
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("mangal serve: decode %s %s: %w", method, path, err)
	}
	return nil
}

func watchPath(id int64) string {
	return "/api/watches/" + strconv.FormatInt(id, 10)
}

func (r *Remote) Add(ctx context.Context, opts AddOptions) (Watch, error) {
	var w Watch
	err := r.do(ctx, http.MethodPost, "/api/watches", addRequest(opts), &w)
	return w, err
}

func (r *Remote) List(ctx context.Context) ([]Watch, error) {
	var list []Watch
	err := r.do(ctx, http.MethodGet, "/api/watches", nil, &list)
	return list, err
}

func (r *Remote) Get(ctx context.Context, id int64) (Watch, error) {
	var w Watch
	err := r.do(ctx, http.MethodGet, watchPath(id), nil, &w)
	return w, err
}

func (r *Remote) Remove(ctx context.Context, id int64) error {
	return r.do(ctx, http.MethodDelete, watchPath(id), nil, nil)
}

func (r *Remote) SetEnabled(ctx context.Context, id int64, enabled bool) (Watch, error) {
	var w Watch
	err := r.do(ctx, http.MethodPatch, watchPath(id), PatchRequest{Enabled: &enabled}, &w)
	return w, err
}

func (r *Remote) Update(ctx context.Context, id int64, opts UpdateOptions) (Watch, error) {
	var w Watch
	err := r.do(ctx, http.MethodPatch, watchPath(id), patchRequest(opts), &w)
	return w, err
}

func (r *Remote) Relink(ctx context.Context, id int64, opts RelinkOptions) (Watch, error) {
	var w Watch
	err := r.do(ctx, http.MethodPost, watchPath(id)+"/relink", opts, &w)
	return w, err
}

func (r *Remote) Stale(ctx context.Context, since time.Duration) ([]Watch, error) {
	var list []Watch
	err := r.do(ctx, http.MethodGet, "/api/watches/stale?since="+url.QueryEscape(since.String()), nil, &list)
	return list, err
}

func (r *Remote) Runs(ctx context.Context, filter RunFilter) ([]CheckRun, error) {
	q := url.Values{}
	if !filter.Since.IsZero() {
		q.Set("since", filter.Since.Format(time.RFC3339Nano))
	}
	if filter.Watch != 0 {
		q.Set("watch", strconv.FormatInt(filter.Watch, 10))
	}
	if filter.Provider != "" {
		q.Set("provider", filter.Provider)
	}
	if filter.MangaID != "" {
		q.Set("manga_id", filter.MangaID)
	}
	if filter.Limit != 0 {
		q.Set("limit", strconv.Itoa(filter.Limit))
	}
	path := "/api/runs"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var runs []CheckRun
	err := r.do(ctx, http.MethodGet, path, nil, &runs)
	return runs, err
}

func (r *Remote) RunDetails(ctx context.Context, id int64) (CheckRun, error) {
	var run CheckRun
	err := r.do(ctx, http.MethodGet, "/api/runs/"+strconv.FormatInt(id, 10), nil, &run)
	return run, err
}

func (r *Remote) RemoveRun(ctx context.Context, id int64) (RemovedRun, error) {
	var removed RemovedRun
	err := r.do(ctx, http.MethodDelete, "/api/watch-runs/"+strconv.FormatInt(id, 10), nil, &removed)
	return removed, err
}

func (r *Remote) Status(ctx context.Context) (Status, error) {
	var st Status
	err := r.do(ctx, http.MethodGet, "/api/status", nil, &st)
	return st, err
}

// Check asks the server to check and waits until it's done.
func (r *Remote) Check(ctx context.Context, ids []int64, opts CheckOptions) (Report, error) {
	req := CheckRequest{IDs: ids, Gaps: opts.Gaps, DryRun: opts.DryRun}
	if opts.DryRun {
		var report Report
		err := r.do(ctx, http.MethodPost, "/api/check", req, &report)
		return report, err
	}
	var started CheckResponse
	if err := r.do(ctx, http.MethodPost, "/api/check", req, &started); err != nil {
		return Report{}, err
	}
	run, err := r.wait(ctx, started.Run)
	if ctx.Err() != nil {
		// like a local check: cancelled, what was done is recorded and reported
		stop, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		path := "/api/runs/" + strconv.FormatInt(started.Run, 10) + "/cancel"
		if err := r.do(stop, http.MethodPost, path, nil, nil); err != nil && !errors.Is(err, ErrNotFound) {
			return Report{}, err
		}
		run, err = r.wait(stop, started.Run)
	}
	if err != nil {
		return Report{}, err
	}
	report := reportOf(run)
	report.Busy = started.Busy
	return report, nil
}

// wait polls the check run until it's finished.
func (r *Remote) wait(ctx context.Context, id int64) (CheckRun, error) {
	interval := r.PollInterval
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		run, err := r.RunDetails(ctx, id)
		if err != nil {
			return run, err
		}
		if !run.Finished.IsZero() {
			return run, nil
		}
		select {
		case <-ctx.Done():
			return run, ctx.Err()
		case <-ticker.C:
		}
	}
}

func reportOf(run CheckRun) Report {
	report := Report{CheckRun: run, Results: make([]Result, len(run.Results))}
	for i, wr := range run.Results {
		report.Results[i] = Result{WatchRun: wr}
	}
	report.CheckRun.Results = nil
	return report
}

// Subscribe streams `GET /api/events` (server-sent events). The first connection is made
// here (its error returned); when it drops it's made again (from the last event seen)
// until ctx is done.
func (r *Remote) Subscribe(ctx context.Context) (<-chan LiveEvent, error) {
	resp, err := r.events(ctx, 0)
	if err != nil {
		return nil, err
	}
	out := make(chan LiveEvent, 64)
	go func() {
		defer close(out)
		var last uint64
		for {
			last = readEvents(ctx, resp.Body, out, last)
			resp.Body.Close()
			for {
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
				if resp, err = r.events(ctx, last); err == nil {
					break
				}
				var apiErr *APIError
				if errors.As(err, &apiErr) && apiErr.Status < 500 {
					// a token that's not accepted anymore, a server without events
					return
				}
			}
		}
	}()
	return out, nil
}

// events opens the event stream, the events after the Seq since.
func (r *Remote) events(ctx context.Context, since uint64) (*http.Response, error) {
	path := "/api/events"
	if since > 0 {
		path += "?since=" + strconv.FormatUint(since, 10)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.Address+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	if r.Token != "" {
		req.Header.Set("Authorization", "Bearer "+r.Token)
	}
	// no timeout: the stream stays open
	client := &http.Client{}
	if r.Client != nil {
		client.Transport = r.Client.Transport
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		var e ErrorResponse
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		if json.Unmarshal(b, &e) != nil || e.Error == "" {
			e.Error = resp.Status
		}
		return nil, &APIError{Status: resp.StatusCode, Message: e.Error, Code: e.Code}
	}
	return resp, nil
}

// readEvents sends the events of the stream to out until it ends, returns the last Seq
// (the server replays what came after it on a reconnection).
func readEvents(ctx context.Context, body io.Reader, out chan<- LiveEvent, last uint64) uint64 {
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		data, ok := strings.CutPrefix(sc.Text(), "data:")
		if !ok {
			// id:, comments (keep-alives) and the blank line between events
			continue
		}
		var e LiveEvent
		if err := json.Unmarshal([]byte(strings.TrimSpace(data)), &e); err != nil {
			continue
		}
		last = e.Seq
		select {
		case out <- e:
		case <-ctx.Done():
			return last
		}
	}
	return last
}

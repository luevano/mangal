package watch

import (
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
// Check runs in the server: OnEvent and OnRun aren't called, the Report has what the
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
	return reportOf(run), nil
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

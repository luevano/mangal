package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/health"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// remoteHealth is the server's health checks (core.Health).
type remoteHealth struct{ c *Client }

// Report is the server's report; a network one runs as its health job, waited for.
func (h remoteHealth) Report(ctx context.Context, opts health.Options) (health.Report, error) {
	params := &apiv1.GetHealthParams{All: opt(opts.All), Network: opt(opts.Network)}
	if len(opts.Only) != 0 {
		params.Only = new(strings.Join(opts.Only, ","))
	}
	r, err := h.c.API.GetHealthWithResponse(ctx, params)
	if err != nil {
		return health.Report{}, fmt.Errorf("mangal serve: %w", err)
	}
	if r.StatusCode() != 202 {
		return answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	}
	job, err := answer(r.StatusCode(), r.Body, r.JSON202, r.JSONDefault)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for err == nil && job.Running {
		if s := deref(job.Status); s != "" {
			core.ReportProgress(ctx, library.Progress{Phase: s})
		}
		select {
		case <-ctx.Done():
			return health.Report{}, ctx.Err()
		case <-tick.C:
		}
		var jr *apiv1.GetHealthJobResponse
		if jr, err = h.c.API.GetHealthJobWithResponse(ctx, job.Id); err != nil {
			return health.Report{}, fmt.Errorf("mangal serve: %w", err)
		}
		job, err = answer(jr.StatusCode(), jr.Body, jr.JSON200, jr.JSONDefault)
	}
	switch {
	case err != nil:
		return health.Report{}, err
	case job.Error != nil:
		return health.Report{}, fmt.Errorf("the server's health check: %s", *job.Error)
	case job.Report == nil:
		return health.Report{}, fmt.Errorf("the server's health check %d has no report", job.Id)
	}
	return *job.Report, nil
}

func (h remoteHealth) Finding(ctx context.Context, key string, network bool) (health.Finding, error) {
	check, subject, err := health.SplitKey(key)
	if err != nil {
		return health.Finding{}, fmt.Errorf("%s: %w", err, core.ErrInvalid)
	}
	report, err := h.Report(ctx, health.Options{Only: []string{check}, Network: network, All: true})
	if err != nil {
		return health.Finding{}, err
	}
	f, ok := report.Find(check + "/" + subject)
	if !ok {
		return health.Finding{}, &core.NotFoundError{What: fmt.Sprintf("no finding %s on the server now", key)}
	}
	return f, nil
}

// Dismiss dismisses the findings on the server: those at hand (Known, Fallback) are
// returned as they are, the others with their key only.
func (h remoteHealth) Dismiss(ctx context.Context, req core.DismissReq) ([]health.Finding, error) {
	r, err := h.c.API.DismissHealthFindingsWithResponse(ctx, apiv1.DismissRequest{Dismiss: &req.Keys, Network: opt(req.Network)})
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	res, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	if err != nil {
		return nil, err
	}
	known := health.Report{Findings: append(slices.Clone(req.Known), req.Fallback...)}
	out := make([]health.Finding, len(res.Dismissed))
	for i, key := range res.Dismissed {
		f, ok := known.Find(key)
		if !ok {
			f = health.Finding{Key: key}
		}
		f.Dismissed = true
		out[i] = f
	}
	return out, nil
}

func (h remoteHealth) Undismiss(ctx context.Context, keys ...string) error {
	r, err := h.c.API.DismissHealthFindingsWithResponse(ctx, apiv1.DismissRequest{Undismiss: &keys})
	if err != nil {
		return fmt.Errorf("mangal serve: %w", err)
	}
	_, err = answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	return err
}

// remoteLogs are the server's log files (core.Logs).
type remoteLogs struct{ c *Client }

func (l remoteLogs) Files(ctx context.Context) ([]log.File, error) {
	r, err := l.c.API.ListLogFilesWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	list, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	if err != nil {
		return nil, err
	}
	out := make([]log.File, len(list))
	for i, f := range list {
		out[i] = log.File{Name: f.Name, Path: f.Path, Size: f.Size, ModTime: f.Modified, Closed: f.Closed, Compressed: f.Compressed}
	}
	return out, nil
}

// Entries are the server's entries of the query (a request per file asked for); the
// server matches the grep its own way (no CaseSensitive).
func (l remoteLogs) Entries(ctx context.Context, q core.LogQuery, fn func(log.Entry) error) error {
	files := q.Files
	if len(files) == 0 {
		files = []string{""}
	}
	for _, file := range files {
		params := &apiv1.ListLogsParams{Level: opt(q.Level), Grep: opt(q.Grep), File: opt(file), Limit: opt(q.Limit)}
		if !q.Since.IsZero() {
			params.Since = new(q.Since.Format(time.RFC3339Nano))
		}
		if !q.Until.IsZero() {
			params.Until = new(q.Until.Format(time.RFC3339Nano))
		}
		if q.Limit == 0 {
			params.Limit = new(core.MaxLogLimit)
		}
		r, err := l.c.API.ListLogsWithResponse(ctx, params)
		if err != nil {
			return fmt.Errorf("mangal serve: %w", err)
		}
		list, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
		if err != nil {
			return err
		}
		for _, e := range list {
			level, _ := log.ParseLevel(e.Level)
			if err := fn(log.Entry{Time: e.Time, Level: level, Message: e.Message, File: e.File}); err != nil {
				return err
			}
		}
	}
	return nil
}

// Follow tells fn the server's new entries of the query (its log event topic, an admin's)
// until ctx is done or fn fails.
func (l remoteLogs) Follow(ctx context.Context, q core.LogQuery, fn func(log.Entry) error) error {
	filter, err := q.Filter()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var fnErr error
	err = l.c.Events(ctx, []string{"log"}, 0, func(ev Event) {
		var e apiv1.LogEntry
		if fnErr != nil || ev.Topic != "log" || json.Unmarshal(ev.Data, &e) != nil {
			return
		}
		level, _ := log.ParseLevel(e.Level)
		if entry := (log.Entry{Time: e.Time, Level: level, Message: e.Message, File: e.File}); filter.Match(entry) {
			if fnErr = fn(entry); fnErr != nil {
				cancel()
			}
		}
	})
	if fnErr != nil {
		return fnErr
	}
	return err
}

// remoteProviders are the server's providers (core.Providers).
type remoteProviders struct{ c *Client }

func providerOf(p apiv1.Provider) libmangal.ProviderInfo {
	return libmangal.ProviderInfo{ID: p.Id, Name: p.Name, Version: p.Version, Description: deref(p.Description),
		Website: deref(p.Website), Icon: deref(p.Icon), Mirrors: deref(p.Mirrors)}
}

func (p remoteProviders) List(ctx context.Context) ([]libmangal.ProviderInfo, error) {
	r, err := p.c.API.ListProvidersWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	list, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	if err != nil {
		return nil, err
	}
	out := make([]libmangal.ProviderInfo, len(list))
	for i, info := range list {
		out[i] = providerOf(info)
	}
	return out, nil
}

func (p remoteProviders) Info(ctx context.Context, id string) (libmangal.ProviderInfo, error) {
	r, err := p.c.API.GetProviderInfoWithResponse(ctx, id)
	if err != nil {
		return libmangal.ProviderInfo{}, fmt.Errorf("mangal serve: %w", err)
	}
	info, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	return providerOf(info), err
}

// UpdateProviders updates the server's installed providers (git pull of each, an
// admin's); nothing is installed.
func (c *Client) UpdateProviders(ctx context.Context) (apiv1.ProvidersUpdate, error) {
	r, err := c.API.UpdateProvidersWithResponse(ctx)
	if err != nil {
		return apiv1.ProvidersUpdate{}, fmt.Errorf("mangal serve: %w", err)
	}
	return answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
}

// remoteConfig is the server's config (core.Config): the keys an admin may change over
// HTTP, secrets and the risky keys aren't there for the others.
type remoteConfig struct{ c *Client }

func configOf(e apiv1.ConfigEntry) core.ConfigEntry {
	out := core.ConfigEntry{Key: e.Key, Value: e.Value, Default: e.Default, Description: e.Description, Secret: e.Secret,
		Values: deref(e.Values), Env: deref(e.Env), Stored: string(e.Stored), Provider: deref(e.Provider),
		Source: config.Source{Kind: string(e.Source.Kind), Name: deref(e.Source.Name)}, Applies: string(e.Applies),
		Restart: deref(e.Restart), Risky: deref(e.Risky), Side: config.Side(e.Key)}
	if e.Kind != nil {
		out.Kind = string(*e.Kind)
	}
	// a server before API 1.1 doesn't say: this mangal's rule
	if e.Side != nil {
		out.Side = string(*e.Side)
	}
	return out
}

func (cf remoteConfig) List(ctx context.Context) ([]core.ConfigEntry, error) {
	r, err := cf.c.API.ListConfigWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	list, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	if err != nil {
		return nil, err
	}
	out := make([]core.ConfigEntry, len(list))
	for i, e := range list {
		out[i] = configOf(e)
	}
	return out, nil
}

func (cf remoteConfig) Get(ctx context.Context, key string) (core.ConfigEntry, error) {
	list, err := cf.List(ctx)
	if err != nil {
		return core.ConfigEntry{}, err
	}
	if i := slices.IndexFunc(list, func(e core.ConfigEntry) bool { return e.Key == key }); i >= 0 {
		return list[i], nil
	}
	return core.ConfigEntry{}, &core.NotFoundError{What: fmt.Sprintf("no config key %q on the server", key)}
}

func (cf remoteConfig) Set(ctx context.Context, key string, value any) (core.ConfigEntry, error) {
	r, err := cf.c.API.SetConfigWithResponse(ctx, key, apiv1.ConfigSetRequest{Value: value})
	if err != nil {
		return core.ConfigEntry{}, fmt.Errorf("mangal serve: %w", err)
	}
	e, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	return configOf(e), err
}

func (cf remoteConfig) History(ctx context.Context, key string, limit int) ([]core.ConfigChange, error) {
	r, err := cf.c.API.ListConfigHistoryWithResponse(ctx, &apiv1.ListConfigHistoryParams{Key: opt(key), Limit: opt(limit)})
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	list, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	if err != nil {
		return nil, err
	}
	out := make([]core.ConfigChange, len(list))
	for i, ch := range list {
		out[i] = core.ConfigChange{ID: ch.Id, Key: ch.Key, Old: ch.Old, New: ch.New, Who: ch.Who, At: ch.At}
	}
	return out, nil
}

func (cf remoteConfig) Undo(ctx context.Context, id int64) (core.ConfigEntry, error) {
	r, err := cf.c.API.UndoConfigChangeWithResponse(ctx, id)
	if err != nil {
		return core.ConfigEntry{}, fmt.Errorf("mangal serve: %w", err)
	}
	e, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	return configOf(e), err
}

func (remoteConfig) Export(context.Context) ([]byte, error) {
	return nil, unsupported("exporting the server's settings")
}

func (remoteConfig) ImportPlan(context.Context, []byte) ([]config.ImportChange, error) {
	return nil, unsupported("importing settings into the server")
}

func (remoteConfig) Import(context.Context, []config.ImportChange) (int64, error) {
	return 0, unsupported("importing settings into the server")
}

// remoteAccounts are the server's accounts as it last checked them (core.Accounts).
type remoteAccounts struct{ c *Client }

func (a remoteAccounts) List(ctx context.Context, _ core.AccountsOptions) ([]core.Account, error) {
	r, err := a.c.API.ListAccountsWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	list, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	if err != nil {
		return nil, err
	}
	out := make([]core.Account, len(list))
	for i, e := range list {
		out[i] = core.Account{Account: metadata.Account{Service: e.Service, User: deref(e.User), URL: deref(e.Url), Status: e.Status,
			Expires: deref(e.Expires), Details: deref(e.Details)}, Provider: deref(e.Provider), Checked: deref(e.Checked), Error: deref(e.Error)}
	}
	return out, nil
}

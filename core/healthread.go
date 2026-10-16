package core

import (
	"context"
	"fmt"

	"github.com/luevano/mangal/v5/health"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
)

// Health runs the health checks and keeps what's dismissed; the fixes are Changes
// (HealthFixReq). Progress goes through the ctx (WithProgress).
type Health interface {
	Report(ctx context.Context, opts health.Options) (health.Report, error)
	// Finding is the finding with the key there now, from a run of its check (with the
	// network checks too when network), a *NotFoundError when it isn't there.
	Finding(ctx context.Context, key string, network bool) (health.Finding, error)
	// Dismiss dismisses the findings with the keys, which must be there now (Known, or
	// a run of their checks), and returns them.
	Dismiss(ctx context.Context, req DismissReq) ([]health.Finding, error)
	// Undismiss brings dismissed findings back, a *NotFoundError for one that isn't.
	Undismiss(ctx context.Context, keys ...string) error
}

// DismissReq are findings to dismiss.
type DismissReq struct {
	Keys []string `json:"keys"`
	// Network runs the network checks too to find them.
	Network bool `json:"network,omitempty"`
	// Known are findings at hand (a report just shown): those dismissed as they are,
	// without a run of their checks.
	Known []health.Finding `json:"-"`
	// Fallback are findings looked in for those a run doesn't find (the last network
	// report).
	Fallback []health.Finding `json:"-"`
}

type localHealth struct {
	core *Core
}

func (h *localHealth) checker(ctx context.Context) (health.Checker, error) {
	m, err := h.core.env.Watches(ctx)
	if err != nil {
		return health.Checker{}, err
	}
	checker, err := h.core.env.Checker(m)
	if err != nil {
		return checker, err
	}
	if f := progressOf(ctx); f != nil {
		checker.OnProgress = func(msg string) { f(library.Progress{Phase: msg}) }
	}
	return checker, nil
}

func (h *localHealth) Report(ctx context.Context, opts health.Options) (health.Report, error) {
	checker, err := h.checker(ctx)
	if err != nil {
		return health.Report{}, err
	}
	return checker.Run(ctx, opts)
}

func (h *localHealth) Finding(ctx context.Context, key string, network bool) (health.Finding, error) {
	check, subject, err := health.SplitKey(key)
	if err != nil {
		return health.Finding{}, invalid("%s", err)
	}
	report, err := h.Report(ctx, health.Options{Only: []string{check}, Network: network, All: true})
	if err != nil {
		return health.Finding{}, err
	}
	f, ok := report.Find(check + "/" + subject)
	if !ok {
		return health.Finding{}, noFinding(key, check, network)
	}
	return f, nil
}

func noFinding(key, check string, network bool) error {
	what := fmt.Sprintf("no finding %s now (mangal health shows the keys", key)
	if health.IsNetwork(check) && !network {
		what += "; a network check's needs --network"
	}
	return &NotFoundError{What: what + ")"}
}

func (h *localHealth) ignores() storage.HealthIgnores {
	return storage.HealthIgnores{DB: h.core.env.Storage.Data}
}

func (h *localHealth) Dismiss(ctx context.Context, req DismissReq) ([]health.Finding, error) {
	found := map[string]health.Finding{}
	var only []string
	for _, key := range req.Keys {
		check, subject, err := health.SplitKey(key)
		if err != nil {
			return nil, invalid("%s", err)
		}
		if f, ok := (health.Report{Findings: req.Known}).Find(check + "/" + subject); ok {
			found[key] = f
			continue
		}
		only = append(only, check)
	}
	if len(only) != 0 {
		report, err := h.Report(ctx, health.Options{Only: only, Network: req.Network, All: true})
		if err != nil {
			return nil, err
		}
		for _, key := range req.Keys {
			check, subject, _ := health.SplitKey(key)
			if f, ok := report.Find(check + "/" + subject); ok {
				found[key] = f
			} else if f, ok := (health.Report{Findings: req.Fallback}).Find(check + "/" + subject); ok {
				found[key] = f
			}
		}
	}
	var dismissed []health.Finding
	for _, key := range req.Keys {
		f, ok := found[key]
		if !ok {
			check, _, _ := health.SplitKey(key)
			return dismissed, noFinding(key, check, req.Network)
		}
		check, subject, _ := health.SplitKey(f.Key)
		if err := h.ignores().Dismiss(storage.HealthIgnore{Check: check, Subject: subject, Message: f.Message}); err != nil {
			return dismissed, err
		}
		f.Dismissed = true
		dismissed = append(dismissed, f)
	}
	return dismissed, nil
}

func (h *localHealth) Undismiss(_ context.Context, keys ...string) error {
	for _, key := range keys {
		check, subject, err := health.SplitKey(key)
		if err != nil {
			return invalid("%s", err)
		}
		ok, err := h.ignores().Undismiss(check, subject)
		if err != nil {
			return err
		}
		if !ok {
			return &NotFoundError{What: key + " isn't dismissed"}
		}
	}
	return nil
}

// Finding is Health.Finding.
func (c *Core) Finding(ctx context.Context, key string, network bool) (health.Finding, error) {
	return c.Health.Finding(ctx, key, network)
}

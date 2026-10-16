package base

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/library"
)

// Core is the app's services (core.Default). Swapped in tests.
var Core = core.Default

// Library is the core's Library.
func Library() (core.Library, error) {
	c, err := Core()
	if err != nil {
		return nil, err
	}
	return c.Library, nil
}

// Changes is the core's Changes.
func Changes() (core.Changes, error) {
	c, err := Core()
	if err != nil {
		return nil, err
	}
	return c.Changes, nil
}

// WithStatus is ctx telling status where a change's plan or apply is.
func WithStatus(ctx context.Context, status func(string)) context.Context {
	return core.WithProgress(ctx, func(p library.Progress) { status(p.String()) })
}

// ApplyOpts are a change's apply options telling status while it waits for the library.
func ApplyOpts(status func(string)) core.ApplyOpts {
	return core.ApplyOpts{OnWait: func(b client.LibraryBusy) { status(b.Message()) }}
}

// PlanChange plans the change (its progress on the Load label), then done of the plan.
func PlanChange(ctx context.Context, label string, ch core.Change, done func(core.Plan) tea.Msg) tea.Cmd {
	return LoadWithStatus(ctx, label, func(ctx context.Context, status func(string)) (core.Plan, error) {
		changes, err := Changes()
		if err != nil {
			return core.Plan{}, err
		}
		return changes.Plan(WithStatus(ctx, status), ch)
	}, done)
}

// ApplyPlan applies the plan with the status telling where it is.
func ApplyPlan(ctx context.Context, p core.Plan, status func(string)) (core.OpResult, error) {
	changes, err := Changes()
	if err != nil {
		return core.OpResult{}, err
	}
	return changes.ApplyPlan(WithStatus(ctx, status), p, ApplyOpts(status))
}

// ApplyChange plans and applies the change (confirmed by the user as the screen says it).
func ApplyChange(ctx context.Context, ch core.Change, status func(string)) (core.OpResult, error) {
	changes, err := Changes()
	if err != nil {
		return core.OpResult{}, err
	}
	return changes.Apply(WithStatus(ctx, status), ch, ApplyOpts(status))
}

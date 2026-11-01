package remote

import (
	"context"
	"fmt"

	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// remoteKinds are the server's maintenance jobs (core.JobKinds).
type remoteKinds struct{ c *Client }

// Kinds are the server's maintenance jobs: its schedules, runs and settings.
func (c *Client) Kinds() core.JobKinds { return remoteKinds{c} }

func (k remoteKinds) List(ctx context.Context) ([]core.JobKind, error) {
	r, err := k.c.API.ListJobKindsWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	return answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
}

func (k remoteKinds) Get(ctx context.Context, kind jobs.Kind) (core.JobKind, error) {
	r, err := k.c.API.GetJobKindWithResponse(ctx, kind.String())
	if err != nil {
		return core.JobKind{}, fmt.Errorf("mangal serve: %w", err)
	}
	return answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
}

func (k remoteKinds) Run(ctx context.Context, kind jobs.Kind, all bool) (core.Job, error) {
	r, err := k.c.API.RunJobKindWithResponse(ctx, kind.String(), apiv1.JobKindRunRequest{All: &all})
	if err != nil {
		return core.Job{}, fmt.Errorf("mangal serve: %w", err)
	}
	job, err := answer(r.StatusCode(), r.Body, r.JSON202, r.JSONDefault)
	if err != nil {
		return core.Job{}, err
	}
	return k.c.Jobs().keep(jobOf(job), false), nil
}

func (k remoteKinds) Pause(ctx context.Context, kind jobs.Kind, paused bool) (core.JobKind, error) {
	if paused {
		r, err := k.c.API.PauseJobKindWithResponse(ctx, kind.String())
		if err != nil {
			return core.JobKind{}, fmt.Errorf("mangal serve: %w", err)
		}
		return answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	}
	r, err := k.c.API.ResumeJobKindWithResponse(ctx, kind.String())
	if err != nil {
		return core.JobKind{}, fmt.Errorf("mangal serve: %w", err)
	}
	return answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
}

func (k remoteKinds) Set(ctx context.Context, kind jobs.Kind, s core.JobSettings) (core.JobKind, error) {
	r, err := k.c.API.SetJobKindSettingsWithResponse(ctx, kind.String(), s)
	if err != nil {
		return core.JobKind{}, fmt.Errorf("mangal serve: %w", err)
	}
	return answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
}

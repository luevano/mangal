package jobs

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/watch"
)

// pollBusy is how often a check waiting for another mangal looks again, swapped in tests.
var pollBusy = 2 * time.Second

// CheckSpec is a check of the watches with the ids (of providers) through m, as a job
// of those providers. summary words its report for the job's last message.
func CheckSpec(m watch.Manager, ids []int64, providers []string, opts watch.CheckOptions, title, origin string, summary func(watch.Report) string) Spec {
	return Spec{
		Kind:      KindCheck,
		Title:     title,
		Providers: providers,
		Origin:    origin,
		Run: func(ctx context.Context, p *Progress) error {
			return runCheck(ctx, p, m, ids, providers, opts, summary)
		},
	}
}

func runCheck(ctx context.Context, p *Progress, m watch.Manager, ids []int64, providers []string, opts watch.CheckOptions, summary func(watch.Report) string) error {
	onLive := opts.OnLive
	opts.OnLive = func(e watch.LiveEvent) {
		switch e.Kind {
		case watch.LiveProviderDone:
			p.Release(e.Provider)
		case watch.LiveStatus:
			p.Note(e.Message)
		case watch.LiveProgress:
			if e.Pages > 0 {
				p.Part(float64(e.PagesDone) / float64(e.Pages))
			}
		}
		if e.Message != "" && e.Kind != watch.LiveStatus {
			p.Message(e.Message)
		}
		if onLive != nil {
			onLive(e)
		}
	}
	for {
		if err := waitFree(ctx, p, providers); err != nil {
			return err
		}
		p.Running("checking")
		report, err := m.Check(ctx, ids, opts)
		if errors.Is(err, watch.ErrBusy) {
			// another mangal took them between the look and the check
			if err := sleep(ctx, pollBusy); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if summary != nil {
			p.Message(summary(report))
		}
		return nil
	}
}

// waitFree waits while another holder (another mangal, or work of this one that isn't a
// job) has one of the providers' locks, the job Waiting meanwhile.
func waitFree(ctx context.Context, p *Progress, providers []string) error {
	for {
		busy, err := busyProvider(providers)
		if err != nil {
			return err
		}
		if busy == nil {
			return nil
		}
		p.Waiting(DescribeBusy(*busy))
		if err := sleep(ctx, pollBusy); err != nil {
			return err
		}
	}
}

// busyProvider is the first of the providers whose lock someone holds, nil when none.
func busyProvider(providers []string) (*client.ProviderBusy, error) {
	store, err := lockStore()
	if err != nil {
		return nil, err
	}
	held, err := store.Holders(client.ProviderLockPrefix, client.ProviderLockStale)
	if err != nil {
		return nil, err
	}
	for _, h := range held {
		provider := strings.TrimPrefix(h.Name, client.ProviderLockPrefix)
		if slices.Contains(providers, provider) {
			return &client.ProviderBusy{Provider: provider, Holder: h, Watch: h.Purpose == client.LockForWatch}, nil
		}
	}
	return nil, nil
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

package watch

import (
	"context"
	"time"
)

// Manager manages the watches: the local Service, or a running `mangal serve` through its
// API (the TUI uses whichever there is).
type Manager interface {
	Add(ctx context.Context, opts AddOptions) (Watch, error)
	List(ctx context.Context) ([]Watch, error)
	Get(ctx context.Context, id int64) (Watch, error)
	Remove(ctx context.Context, id int64) error
	SetEnabled(ctx context.Context, id int64, enabled bool) (Watch, error)
	Update(ctx context.Context, id int64, opts UpdateOptions) (Watch, error)
	// Check checks the watches now (every due one without ids). OnEvent may be dropped by
	// a remote manager.
	Check(ctx context.Context, ids []int64, opts CheckOptions) (Report, error)
	Stale(ctx context.Context, since time.Duration) ([]Watch, error)
	Runs(ctx context.Context, filter RunFilter) ([]CheckRun, error)
	RunDetails(ctx context.Context, id int64) (CheckRun, error)
	Status(ctx context.Context) (Status, error)
}

// Status is what the manager is doing.
type Status struct {
	// Server is true for a running `mangal serve`, Address its API.
	Server  bool   `json:"server"`
	Address string `json:"address,omitempty"`
	Version string `json:"version,omitempty"`
	// Started is when the server started.
	Started time.Time `json:"started,omitzero"`
	// Checking is true while a check runs (from any process), since Since.
	Checking bool      `json:"checking"`
	Since    time.Time `json:"since,omitzero"`
	// Run is the check run the server is doing (scheduled or asked through its API), 0
	// when none or not known.
	Run int64 `json:"run,omitempty"`
	// NextCheck is the next scheduled check (the earliest due watch), zero without
	// enabled watches or a scheduler.
	NextCheck time.Time `json:"next_check,omitzero"`
}

var _ Manager = (*Service)(nil)

// Status of the local service: no server, checking when any process holds the check lock.
func (s *Service) Status(ctx context.Context) (Status, error) {
	var st Status
	h, held, err := s.Watches.Holder(lockName, s.Options.LockStale)
	if err != nil {
		return st, err
	}
	st.Checking, st.Since = held, h.Acquired
	all, err := s.List(ctx)
	if err != nil {
		return st, err
	}
	for _, w := range all {
		if w.Enabled && !w.ProviderMissing && (st.NextCheck.IsZero() || w.NextCheck.Before(st.NextCheck)) {
			st.NextCheck = w.NextCheck
		}
	}
	return st, nil
}

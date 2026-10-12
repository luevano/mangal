package watch

import (
	"context"
	"strings"
	"time"

	"github.com/luevano/mangal/v5/client"
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
	// Relink points the watch to another manga, keeping the watch.
	Relink(ctx context.Context, id int64, opts RelinkOptions) (Watch, error)
	// Check checks the watches now (every due one without ids). OnEvent may be dropped by
	// a remote manager.
	Check(ctx context.Context, ids []int64, opts CheckOptions) (Report, error)
	Stale(ctx context.Context, since time.Duration) ([]Watch, error)
	Runs(ctx context.Context, filter RunFilter) ([]CheckRun, error)
	RunDetails(ctx context.Context, id int64) (CheckRun, error)
	// RemoveRun removes a watch run from the history (its check run too when it had no
	// other), after a snapshot of the data db.
	RemoveRun(ctx context.Context, id int64) (RemovedRun, error)
	Status(ctx context.Context) (Status, error)
	// Subscribe is the live progress of the checks until ctx is done (the channel is
	// closed then): the recent events of the running checks first, then each one as it
	// happens. Locally only the checks of this process are seen.
	Subscribe(ctx context.Context) (<-chan LiveEvent, error)
}

// Status is what the manager is doing.
type Status struct {
	// Server is true for a running `mangal serve`, Address its API.
	Server  bool   `json:"server"`
	Address string `json:"address,omitempty"`
	Version string `json:"version,omitempty"`
	// Started is when the server started.
	Started time.Time `json:"started,omitzero"`
	// Checking is true while a check runs (from any process), since Since (the earliest).
	Checking bool      `json:"checking"`
	Since    time.Time `json:"since,omitzero"`
	// Providers are the ones being checked (from any process), by provider.
	Providers []ProviderStatus `json:"providers,omitempty"`
	// Run is a check run the server is doing (scheduled or asked through its API), 0
	// when none or not known; Runs are all of them.
	Run  int64   `json:"run,omitempty"`
	Runs []int64 `json:"runs,omitempty"`
	// NextCheck is the next scheduled check (the earliest due watch), zero without
	// enabled watches or a scheduler.
	NextCheck time.Time `json:"next_check,omitzero"`
}

var _ Manager = (*Service)(nil)

// Status of the local service: no server, the providers any process checks (what they do
// for this process's checks).
func (s *Service) Status(ctx context.Context) (Status, error) {
	var st Status
	held, err := s.Watches.Holders(lockPrefix, s.lockStale())
	if err != nil {
		return st, err
	}
	active := map[string]ProviderStatus{}
	if s.Events != nil {
		for _, ps := range s.Events.Active() {
			active[ps.Provider] = ps
		}
	}
	for _, h := range held {
		if h.Purpose != client.LockForWatch {
			// a download, not a check
			continue
		}
		provider := strings.TrimPrefix(h.Name, lockPrefix)
		ps, ok := active[provider]
		if !ok || ps.PID != h.PID || ps.Host != h.Host {
			ps = ProviderStatus{Provider: provider, PID: h.PID, Host: h.Host}
		}
		ps.Since = h.Acquired
		st.Providers = append(st.Providers, ps)
		if !st.Checking || h.Acquired.Before(st.Since) {
			st.Checking, st.Since = true, h.Acquired
		}
	}
	all, err := s.List(ctx)
	if err != nil {
		return st, err
	}
	for _, w := range all {
		if w.Scheduled() && (st.NextCheck.IsZero() || w.NextScheduled.Before(st.NextCheck)) {
			st.NextCheck = w.NextScheduled
		}
	}
	return st, nil
}

// Provider is the status of the provider, false when it's not being checked.
func (st Status) Provider(provider string) (ProviderStatus, bool) {
	for _, ps := range st.Providers {
		if ps.Provider == provider {
			return ps, true
		}
	}
	return ProviderStatus{}, false
}

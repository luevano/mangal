package watch

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/storage"
)

// Runs lists the check runs (with what each watch got), latest first.
func (s *Service) Runs(ctx context.Context, filter RunFilter) ([]CheckRun, error) {
	return s.Watches.Runs(filter)
}

// RemovedRun is a watch run removed from the history.
type RemovedRun struct {
	// Run is the removed one, without its chapters.
	Run WatchRun `json:"run"`
	// CheckRunRemoved is true when its check run had nothing else and went too.
	CheckRunRemoved bool `json:"check_run_removed,omitempty"`
	// Snapshot is the data db's snapshot taken before ("mangal library undo" restores it).
	Snapshot string `json:"snapshot,omitempty"`
}

// RemoveRun removes a watch run (the check of one watch in a check run) and its chapters
// from the history, as an operation "mangal library undo" can undo. A check run left
// without watch runs goes too. Unknown = storage.ErrRunNotFound.
func (s *Service) RemoveRun(ctx context.Context, id int64) (RemovedRun, error) {
	removed, err := s.RemoveRuns(ctx, []int64{id})
	if len(removed) == 0 {
		return RemovedRun{}, err
	}
	return removed[0], err
}

// RemoveRuns removes the watch runs as RemoveRun does, as one operation (every one's
// Snapshot is its snapshot). Nothing is removed when one is unknown or unfinished.
func (s *Service) RemoveRuns(ctx context.Context, ids []int64) ([]RemovedRun, error) {
	// no snapshot for nothing
	crashed := map[int64]bool{}
	for _, id := range ids {
		if _, finished, err := s.Watches.WatchRun(id); err != nil {
			return nil, fmt.Errorf("remove %w", err)
		} else if !finished {
			// a check that crashed never finishes: running, it holds provider locks
			checking, err := s.checking()
			if err != nil {
				return nil, err
			}
			if checking {
				return nil, fmt.Errorf("remove watch run %d: %w", id, storage.ErrRunUnfinished)
			}
			crashed[id] = true
		}
	}
	var removed []RemovedRun
	do := func() error {
		for _, id := range ids {
			var r RemovedRun
			var err error
			remove := s.Watches.RemoveWatchRun
			if crashed[id] {
				remove = s.Watches.RemoveCrashedWatchRun
			}
			if r.Run, r.CheckRunRemoved, err = remove(id); err != nil {
				return err
			}
			removed = append(removed, r)
		}
		return nil
	}
	if s.Operation == nil || len(ids) == 0 {
		return removed, do()
	}
	strs := make([]string, len(ids))
	for i, id := range ids {
		strs[i] = strconv.FormatInt(id, 10)
	}
	summary := fmt.Sprintf("remove watch run %s from the history", strs[0])
	if len(ids) > 1 {
		summary = fmt.Sprintf("remove watch runs %s from the history", strings.Join(strs, ", "))
	}
	snapshot, err := s.Operation("history", summary, do)
	for i := range removed {
		removed[i].Snapshot = snapshot
	}
	return removed, err
}

// checking is true when a watch check runs, here or in another process (it holds
// provider locks).
func (s *Service) checking() (bool, error) {
	holders, err := s.Watches.Holders(lockPrefix, s.lockStale())
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(holders, func(h storage.LockHolder) bool { return h.Purpose == client.LockForWatch }), nil
}

// RunDetails is the check run with the id, ErrNotFound if none.
func (s *Service) RunDetails(ctx context.Context, id int64) (CheckRun, error) {
	run, found, err := s.Watches.Run(id)
	if err != nil {
		return CheckRun{}, err
	}
	if !found {
		return CheckRun{}, fmt.Errorf("check run %d: %w", id, ErrNotFound)
	}
	return run, nil
}

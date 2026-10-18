package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// CheckRun is a pass over some watches.
type CheckRun struct {
	ID int64 `json:"id"`
	// Kind is scheduled, manual, gaps or import.
	Kind     string    `json:"kind"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished,omitzero"`
	// Watches is how many were checked, NewChapters how many chapters they downloaded,
	// Errors how many failed.
	Watches     int `json:"watches"`
	NewChapters int `json:"new_chapters"`
	Errors      int `json:"errors"`

	Results []WatchRun `json:"results,omitempty"`
}

// Statuses of a WatchRun.
const (
	StatusNew     = "new"
	StatusNone    = "none"
	StatusError   = "error"
	StatusSkipped = "skipped"
)

// WatchRun is the check of a watch in a CheckRun.
type WatchRun struct {
	ID       int64 `json:"id"`
	CheckRun int64 `json:"check_run"`
	// Watch is 0 once the watch is removed (the history stays).
	Watch    int64  `json:"watch"`
	Provider string `json:"provider"`
	MangaID  string `json:"manga_id"`
	Title    string `json:"title"`
	// From is the last downloaded chapter the check started from, nil when there was none.
	From *float32 `json:"from,omitempty"`
	// Status is new (chapters downloaded), none, error or skipped.
	Status   string       `json:"status"`
	Error    string       `json:"error,omitempty"`
	Started  time.Time    `json:"started"`
	Finished time.Time    `json:"finished"`
	Chapters []RunChapter `json:"chapters,omitempty"`
	// NewChapters is how many chapters it downloaded: len(Chapters), or more when they
	// weren't all recorded (e.g. history imported from elsewhere with only a count).
	NewChapters int `json:"new_chapters"`
	// Skipped are the chapters the watch's skip list kept it from downloading.
	Skipped []float32 `json:"skipped,omitempty"`
}

// New is how many chapters the check downloaded, see NewChapters.
func (r WatchRun) New() int {
	return max(r.NewChapters, len(r.Chapters))
}

// RunChapter is a chapter a WatchRun downloaded.
type RunChapter struct {
	Number float32 `json:"number"`
	Title  string  `json:"title"`
	Volume float32 `json:"volume"`
	Path   string  `json:"path"`
	URL    string  `json:"url"`
}

// StartRun records the start of a check run, its ID is set.
func (s Watches) StartRun(run CheckRun) (CheckRun, error) {
	s.DB.touch()
	res, err := s.DB.Exec(`INSERT INTO check_runs (kind, started) VALUES (?, ?)`, run.Kind, unix(run.Started))
	if err != nil {
		return run, fmt.Errorf("start check run: %w", err)
	}
	run.ID, err = res.LastInsertId()
	return run, err
}

// FinishRun records the end of a check run and its totals.
func (s Watches) FinishRun(run CheckRun) error {
	s.DB.touch()
	_, err := s.DB.Exec(`UPDATE check_runs SET finished = ?, watches = ?, new_chapters = ?, errors = ? WHERE id = ?`,
		unix(run.Finished), run.Watches, run.NewChapters, run.Errors, run.ID)
	if err != nil {
		return fmt.Errorf("finish check run %d: %w", run.ID, err)
	}
	return nil
}

// RecordWatchRun records the check of a watch with its chapters, its ID is set. A watch
// removed meanwhile is recorded without its watch.
func (s Watches) RecordWatchRun(r WatchRun) (WatchRun, error) {
	s.DB.touch()
	err := s.DB.InTx(func(tx *sql.Tx) error {
		r.NewChapters = r.New()
		res, err := tx.Exec(`INSERT INTO watch_runs (check_run, watch, provider, manga_id, title, from_number, status, error, started, finished, new_chapters, skipped)
VALUES (?, (SELECT id FROM watches WHERE id = ?), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, r.CheckRun, nullID(r.Watch), r.Provider, r.MangaID, r.Title, nullFloat(r.From), r.Status, r.Error,
			unix(r.Started), unix(r.Finished), r.NewChapters, formatNumbers(r.Skipped))
		if err != nil {
			return err
		}
		if r.ID, err = res.LastInsertId(); err != nil {
			return err
		}
		for _, ch := range r.Chapters {
			if _, err := tx.Exec(`INSERT INTO watch_run_chapters (watch_run, number, title, volume, path, url) VALUES (?, ?, ?, ?, ?, ?)`,
				r.ID, ch.Number, ch.Title, ch.Volume, ch.Path, ch.URL); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return r, fmt.Errorf("record the check of %s %q: %w", r.Provider, r.MangaID, err)
	}
	return r, nil
}

var (
	// ErrRunNotFound is a run id that doesn't exist.
	ErrRunNotFound = errors.New("run not found")
	// ErrRunUnfinished is removing a run of a check that's still going.
	ErrRunUnfinished = errors.New("its check is still running")
)

// WatchRun is the watch run with the id (without chapters), and whether its check run
// finished; unknown = ErrRunNotFound.
func (s Watches) WatchRun(id int64) (WatchRun, bool, error) {
	r, finished, err := watchRun(s.DB, id)
	if err != nil {
		return r, false, fmt.Errorf("watch run %d: %w", id, err)
	}
	return r, finished, nil
}

type queryRower interface {
	QueryRow(query string, args ...any) *sql.Row
}

func watchRun(db queryRower, id int64) (r WatchRun, finished bool, err error) {
	var started, finishedAt int64
	var from sql.NullFloat64
	err = db.QueryRow(`SELECT w.check_run, coalesce(w.watch, 0), w.provider, w.manga_id, w.title, w.from_number, w.status, w.started, w.new_chapters, c.finished
FROM watch_runs w JOIN check_runs c ON c.id = w.check_run WHERE w.id = ?`, id).Scan(&r.CheckRun, &r.Watch, &r.Provider,
		&r.MangaID, &r.Title, &from, &r.Status, &started, &r.NewChapters, &finishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, ErrRunNotFound
	}
	r.ID, r.From, r.Started = id, floatPtr(from), fromUnix(started)
	return r, finishedAt != 0, err
}

// RemoveWatchRun removes the watch run with its chapters from the history, and its check
// run's totals; a check run left without watch runs goes too (checkRunGone). It returns the
// removed run (without chapters). Unknown = ErrRunNotFound, a check run that hasn't
// finished = ErrRunUnfinished.
func (s Watches) RemoveWatchRun(id int64) (removed WatchRun, checkRunGone bool, err error) {
	return s.removeWatchRun(id, false)
}

// RemoveCrashedWatchRun is RemoveWatchRun of a run whose check never finished and never
// will (it crashed: nothing holds a provider lock for a watch check).
func (s Watches) RemoveCrashedWatchRun(id int64) (removed WatchRun, checkRunGone bool, err error) {
	return s.removeWatchRun(id, true)
}

func (s Watches) removeWatchRun(id int64, crashed bool) (removed WatchRun, checkRunGone bool, err error) {
	err = s.DB.InTx(func(tx *sql.Tx) error {
		var finished bool
		var err error
		if removed, finished, err = watchRun(tx, id); err != nil {
			return err
		}
		if !finished && !crashed {
			return ErrRunUnfinished
		}
		if _, err := tx.Exec(`DELETE FROM watch_runs WHERE id = ?`, id); err != nil {
			return err
		}
		var left int
		if err := tx.QueryRow(`SELECT count(*) FROM watch_runs WHERE check_run = ?`, removed.CheckRun).Scan(&left); err != nil {
			return err
		}
		if checkRunGone = left == 0; checkRunGone {
			_, err = tx.Exec(`DELETE FROM check_runs WHERE id = ?`, removed.CheckRun)
			return err
		}
		if removed.Status == StatusSkipped {
			return nil
		}
		isError := 0
		if removed.Status == StatusError {
			isError = 1
		}
		_, err = tx.Exec(`UPDATE check_runs SET watches = max(watches - 1, 0), new_chapters = max(new_chapters - ?, 0), errors = max(errors - ?, 0) WHERE id = ?`,
			removed.NewChapters, isError, removed.CheckRun)
		return err
	})
	if err != nil {
		return removed, false, fmt.Errorf("remove watch run %d: %w", id, err)
	}
	return removed, checkRunGone, nil
}

// RunFilter selects check runs: started at or after Since (zero for any), with a check of
// Watch (0 for any, then only its checks are listed), the Limit latest (0 for all).
// Provider (and MangaID with it) select the checks the same way, by what was checked:
// they keep the history of a removed watch.
type RunFilter struct {
	Since    time.Time
	Watch    int64
	Provider string
	MangaID  string
	Limit    int
}

// runFilterWhere is the SQL condition on the watch_runs w of the filter, ?2 to ?4 its args.
const runFilterWhere = `(?2 = 0 OR w.watch = ?2) AND (?3 = '' OR w.provider = ?3) AND (?4 = '' OR w.manga_id = ?4)`

func scanCheckRun(rows *sql.Rows) (CheckRun, error) {
	var r CheckRun
	var started, finished int64
	err := rows.Scan(&r.ID, &r.Kind, &started, &finished, &r.Watches, &r.NewChapters, &r.Errors)
	r.Started, r.Finished = fromUnix(started), fromUnix(finished)
	return r, err
}

// Runs lists the check runs, latest first, with their watch runs and chapters.
func (s Watches) Runs(f RunFilter) ([]CheckRun, error) {
	query := `SELECT id, kind, started, finished, watches, new_chapters, errors FROM check_runs
WHERE started >= ?1 AND (?2 = 0 AND ?3 = '' AND ?4 = '' OR EXISTS (SELECT 1 FROM watch_runs w WHERE w.check_run = check_runs.id AND ` + runFilterWhere + `))
ORDER BY started DESC, id DESC`
	args := []any{unix(f.Since), f.Watch, f.Provider, f.MangaID}
	if f.Limit > 0 {
		query += ` LIMIT ?5`
		args = append(args, f.Limit)
	}
	runs, err := queryAll(s.DB, scanCheckRun, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list check runs: %w", err)
	}
	for i := range runs {
		if runs[i].Results, err = s.watchRuns(runs[i].ID, f); err != nil {
			return nil, err
		}
	}
	return runs, nil
}

// Run is the check run with the id, with its watch runs and chapters.
func (s Watches) Run(id int64) (CheckRun, bool, error) {
	runs, err := queryAll(s.DB, scanCheckRun, `SELECT id, kind, started, finished, watches, new_chapters, errors FROM check_runs WHERE id = ?`, id)
	if err != nil || len(runs) == 0 {
		return CheckRun{}, false, err
	}
	run := runs[0]
	run.Results, err = s.watchRuns(id, RunFilter{})
	return run, err == nil, err
}

func (s Watches) watchRuns(checkRun int64, f RunFilter) ([]WatchRun, error) {
	runs, err := queryAll(s.DB, func(rows *sql.Rows) (WatchRun, error) {
		var r WatchRun
		var from sql.NullFloat64
		var started, finished int64
		var skipped string
		err := rows.Scan(&r.ID, &r.CheckRun, &r.Watch, &r.Provider, &r.MangaID, &r.Title, &from, &r.Status, &r.Error, &started, &finished, &r.NewChapters, &skipped)
		r.From, r.Started, r.Finished = floatPtr(from), fromUnix(started), fromUnix(finished)
		r.Skipped = parseNumbers(skipped)
		return r, err
	}, `SELECT id, check_run, coalesce(watch, 0), provider, manga_id, title, from_number, status, error, started, finished, new_chapters, skipped
FROM watch_runs w WHERE check_run = ?1 AND `+runFilterWhere+` ORDER BY id`, checkRun, f.Watch, f.Provider, f.MangaID)
	if err != nil {
		return nil, fmt.Errorf("watch runs of check run %d: %w", checkRun, err)
	}
	for i := range runs {
		runs[i].Chapters, err = queryAll(s.DB, func(rows *sql.Rows) (RunChapter, error) {
			var ch RunChapter
			err := rows.Scan(&ch.Number, &ch.Title, &ch.Volume, &ch.Path, &ch.URL)
			return ch, err
		}, `SELECT number, title, volume, path, url FROM watch_run_chapters WHERE watch_run = ? ORDER BY number`, runs[i].ID)
		if err != nil {
			return nil, fmt.Errorf("chapters of watch run %d: %w", runs[i].ID, err)
		}
	}
	return runs, nil
}

// runNewChapters gives every watch run its count of new chapters (the existing ones: the
// chapters recorded under it).
const runNewChapters = `ALTER TABLE watch_runs ADD COLUMN new_chapters INTEGER NOT NULL DEFAULT 0;
UPDATE watch_runs SET new_chapters = (SELECT count(*) FROM watch_run_chapters c WHERE c.watch_run = watch_runs.id)`

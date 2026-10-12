package storage

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/luevano/mangal/v5/util/cron"
	"github.com/luevano/mangal/v5/util/proc"
)

// Watches are the mangas mangal keeps up to date and the history of their checks, in the
// data database.
type Watches struct {
	DB *DB
}

// ErrWatchExists is adding a watch of a manga that's already watched.
var ErrWatchExists = errors.New("manga already watched")

// ErrWatchNotFound is a watch id that doesn't exist.
var ErrWatchNotFound = errors.New("watch not found")

// ErrLocked is a lock held by another process (or another holder in this one).
var ErrLocked = errors.New("locked")

// Watch is a watched manga: one provider's manga.
type Watch struct {
	ID       int64  `json:"id"`
	Provider string `json:"provider"`
	MangaID  string `json:"manga_id"`
	// Title is the manga's title on the provider, for display (and the search fallback of
	// providers that can't get a manga by id).
	Title string `json:"title"`
	// AnilistID is the manga's AniList binding (metadata_bindings), 0 for none. Not stored
	// with the watch: client/watch fills it.
	AnilistID int `json:"-"`
	// LibraryManga is the library manga it's downloaded into, 0 until known.
	LibraryManga int64 `json:"library_manga,omitempty"`
	Enabled      bool  `json:"enabled"`
	// Schedule overrides watch.schedule when not empty (a cron schedule, util/cron).
	Schedule string `json:"schedule,omitempty"`
	// Gaps also downloads the chapters below the last downloaded one the library misses.
	Gaps bool `json:"gaps"`
	// From treats the chapters up to it as done (read elsewhere), nil for none.
	From *float32 `json:"from,omitempty"`

	Added        time.Time `json:"added"`
	LastChecked  time.Time `json:"last_checked,omitzero"`
	LastGapCheck time.Time `json:"last_gap_check,omitzero"`
	// LastNew is the newest chapter number the last check that found any downloaded, and when.
	LastNew   *float32  `json:"last_new,omitempty"`
	LastNewAt time.Time `json:"last_new_at,omitzero"`
	LastError string    `json:"last_error,omitempty"`
	// Resumed is when it was last resumed or re-linked: its next check counts from then,
	// the ticks it missed before aren't caught up.
	Resumed time.Time `json:"resumed,omitzero"`
	// Failures is how many checks in a row failed.
	Failures int `json:"failures,omitempty"`
	// NeedsRelink is a watch whose manga id may be wrong (moved from an old provider id,
	// not found by a check, a placeholder id): the scheduler skips it until it's re-linked
	// or a check finds the manga. RelinkReason says why.
	NeedsRelink  bool   `json:"needs_relink,omitempty"`
	RelinkReason string `json:"relink_reason,omitempty"`
	// Retry are the chapters the last check failed to download: the next one tries them
	// again, also when they're below the last downloaded one.
	Retry []float32 `json:"retry,omitempty"`
}

// PlaceholderPrefix starts the manga id of a watch added without one (e.g. "title:Berserk"
// from an offline add): it needs a re-link.
const PlaceholderPrefix = "title:"

// relinkColumn is what's stored: the reason, empty when no re-link is needed.
func (w Watch) relinkColumn() string {
	switch {
	case !w.NeedsRelink:
		return ""
	case w.RelinkReason == "":
		return "needs re-link"
	}
	return w.RelinkReason
}

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

const watchTables = `CREATE TABLE watches (
	id             INTEGER PRIMARY KEY,
	provider       TEXT NOT NULL,
	manga_id       TEXT NOT NULL,
	title          TEXT NOT NULL,
	anilist_id     INTEGER NOT NULL DEFAULT 0,
	library_manga  INTEGER REFERENCES library_mangas (id) ON DELETE SET NULL,
	enabled        INTEGER NOT NULL DEFAULT 1,
	interval       INTEGER,
	gaps           INTEGER NOT NULL DEFAULT 0,
	from_number    REAL,
	added          INTEGER NOT NULL,
	last_checked   INTEGER NOT NULL DEFAULT 0,
	last_gap_check INTEGER NOT NULL DEFAULT 0,
	last_new       REAL,
	last_new_at    INTEGER NOT NULL DEFAULT 0,
	last_error     TEXT NOT NULL DEFAULT '',
	failures       INTEGER NOT NULL DEFAULT 0,
	UNIQUE (provider, manga_id)
);
CREATE INDEX watches_library_manga ON watches (library_manga);
CREATE TABLE check_runs (
	id           INTEGER PRIMARY KEY,
	kind         TEXT NOT NULL,
	started      INTEGER NOT NULL,
	finished     INTEGER NOT NULL DEFAULT 0,
	watches      INTEGER NOT NULL DEFAULT 0,
	new_chapters INTEGER NOT NULL DEFAULT 0,
	errors       INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX check_runs_started ON check_runs (started);
CREATE TABLE watch_runs (
	id          INTEGER PRIMARY KEY,
	check_run   INTEGER NOT NULL REFERENCES check_runs (id) ON DELETE CASCADE,
	watch       INTEGER REFERENCES watches (id) ON DELETE SET NULL,
	provider    TEXT NOT NULL,
	manga_id    TEXT NOT NULL,
	title       TEXT NOT NULL,
	from_number REAL,
	status      TEXT NOT NULL,
	error       TEXT NOT NULL DEFAULT '',
	started     INTEGER NOT NULL,
	finished    INTEGER NOT NULL
);
CREATE INDEX watch_runs_check_run ON watch_runs (check_run);
CREATE INDEX watch_runs_watch ON watch_runs (watch);
CREATE INDEX watch_runs_provider ON watch_runs (provider);
CREATE TABLE watch_run_chapters (
	watch_run INTEGER NOT NULL REFERENCES watch_runs (id) ON DELETE CASCADE,
	number    REAL NOT NULL,
	title     TEXT NOT NULL,
	volume    REAL NOT NULL,
	path      TEXT NOT NULL,
	url       TEXT NOT NULL
);
CREATE INDEX watch_run_chapters_run ON watch_run_chapters (watch_run);
CREATE TABLE locks (
	name      TEXT PRIMARY KEY,
	token     TEXT NOT NULL,
	pid       INTEGER NOT NULL,
	host      TEXT NOT NULL,
	acquired  INTEGER NOT NULL,
	heartbeat INTEGER NOT NULL
)`

// migration 7: what a lock is held for (e.g. a manga's: a watch check or a download).
const lockPurpose = `ALTER TABLE locks ADD COLUMN purpose TEXT NOT NULL DEFAULT ''`

// migrateWatchSchedules (migration 9): the per-watch check interval (seconds) becomes a
// cron schedule, the closest one on wall-clock ticks (cron.FromInterval).
func migrateWatchSchedules(tx *sql.Tx) error {
	if _, err := tx.Exec(`ALTER TABLE watches ADD COLUMN schedule TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT id, interval FROM watches WHERE interval > 0`)
	if err != nil {
		return err
	}
	schedules := map[int64]string{}
	for rows.Next() {
		var id, seconds int64
		if err := rows.Scan(&id, &seconds); err != nil {
			rows.Close()
			return err
		}
		schedules[id] = cron.FromInterval(time.Duration(seconds) * time.Second)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for id, schedule := range schedules {
		if _, err := tx.Exec(`UPDATE watches SET schedule = ? WHERE id = ?`, schedule, id); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`ALTER TABLE watches DROP COLUMN interval`)
	return err
}

func nullFloat(f *float32) sql.NullFloat64 {
	if f == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: float64(*f), Valid: true}
}

func floatPtr(n sql.NullFloat64) *float32 {
	if !n.Valid {
		return nil
	}
	f := float32(n.Float64)
	return &f
}

func nullID(id int64) sql.NullInt64 {
	return sql.NullInt64{Int64: id, Valid: id != 0}
}

// unix is t's unix time, 0 for the zero time.
func unix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func fromUnix(s int64) time.Time {
	if s == 0 {
		return time.Time{}
	}
	return time.Unix(s, 0)
}

const watchColumns = `id, provider, manga_id, title, coalesce(library_manga, 0), enabled, schedule, gaps,
	from_number, added, last_checked, last_gap_check, last_new, last_new_at, last_error, failures, relink, retry, resumed`

func scanWatch(rows *sql.Rows) (Watch, error) {
	var w Watch
	var added, checked, gapChecked, newAt, resumed int64
	var from, lastNew sql.NullFloat64
	var retry string
	err := rows.Scan(&w.ID, &w.Provider, &w.MangaID, &w.Title, &w.LibraryManga, &w.Enabled, &w.Schedule, &w.Gaps,
		&from, &added, &checked, &gapChecked, &lastNew, &newAt, &w.LastError, &w.Failures, &w.RelinkReason, &retry, &resumed)
	w.Retry = parseNumbers(retry)
	w.NeedsRelink = w.RelinkReason != ""
	w.From, w.LastNew = floatPtr(from), floatPtr(lastNew)
	w.Added, w.LastChecked, w.LastGapCheck, w.LastNewAt = fromUnix(added), fromUnix(checked), fromUnix(gapChecked), fromUnix(newAt)
	w.Resumed = fromUnix(resumed)
	return w, err
}

// Add adds a watch, its ID and Added are set. A manga already watched is ErrWatchExists.
func (s Watches) Add(w Watch) (Watch, error) {
	if w.Added.IsZero() {
		w.Added = time.Now()
	}
	var exists int
	if err := s.DB.QueryRow(`SELECT count(*) FROM watches WHERE provider = ? AND manga_id = ?`, w.Provider, w.MangaID).Scan(&exists); err != nil {
		return w, fmt.Errorf("add watch: %w", err)
	}
	if exists != 0 {
		return w, fmt.Errorf("add watch of %s %q: %w", w.Provider, w.MangaID, ErrWatchExists)
	}
	res, err := s.DB.Exec(`INSERT INTO watches (provider, manga_id, title, library_manga, enabled, schedule, gaps, from_number, added, relink)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, w.Provider, w.MangaID, w.Title, nullID(w.LibraryManga), w.Enabled,
		w.Schedule, w.Gaps, nullFloat(w.From), unix(w.Added), w.relinkColumn())
	if err != nil {
		return w, fmt.Errorf("add watch of %s %q: %w", w.Provider, w.MangaID, err)
	}
	w.ID, err = res.LastInsertId()
	return w, err
}

// List lists every watch, by id.
func (s Watches) List() ([]Watch, error) {
	watches, err := queryAll(s.DB, scanWatch, `SELECT `+watchColumns+` FROM watches ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list watches: %w", err)
	}
	return watches, nil
}

// Get is the watch with the id, ErrWatchNotFound if none.
func (s Watches) Get(id int64) (Watch, error) {
	watches, err := queryAll(s.DB, scanWatch, `SELECT `+watchColumns+` FROM watches WHERE id = ?`, id)
	if err != nil {
		return Watch{}, fmt.Errorf("get watch %d: %w", id, err)
	}
	if len(watches) == 0 {
		return Watch{}, fmt.Errorf("watch %d: %w", id, ErrWatchNotFound)
	}
	return watches[0], nil
}

// Find is the watch of the provider's manga, false if it isn't watched.
func (s Watches) Find(provider, mangaID string) (Watch, bool, error) {
	watches, err := queryAll(s.DB, scanWatch, `SELECT `+watchColumns+` FROM watches WHERE provider = ? AND manga_id = ?`, provider, mangaID)
	if err != nil || len(watches) == 0 {
		return Watch{}, false, err
	}
	return watches[0], true, nil
}

// Remove removes the watch, its runs stay in the history.
func (s Watches) Remove(id int64) error {
	res, err := s.DB.Exec(`DELETE FROM watches WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("remove watch %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("watch %d: %w", id, ErrWatchNotFound)
	}
	return nil
}

// Save writes every field of the watch but its provider, manga id and added time.
func (s Watches) Save(w Watch) error {
	res, err := s.DB.Exec(`UPDATE watches SET title = ?, library_manga = ?, enabled = ?, schedule = ?, gaps = ?,
	from_number = ?, last_checked = ?, last_gap_check = ?, last_new = ?, last_new_at = ?, last_error = ?, failures = ?, relink = ?, retry = ?, resumed = ?
WHERE id = ?`, w.Title, nullID(w.LibraryManga), w.Enabled, w.Schedule, w.Gaps,
		nullFloat(w.From), unix(w.LastChecked), unix(w.LastGapCheck), nullFloat(w.LastNew), unix(w.LastNewAt), w.LastError, w.Failures,
		w.relinkColumn(), formatNumbers(w.Retry), unix(w.Resumed), w.ID)
	if err != nil {
		return fmt.Errorf("save watch %d: %w", w.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("watch %d: %w", w.ID, ErrWatchNotFound)
	}
	return nil
}

// Relink points the watch to another manga (provider, manga id, title), keeping the rest:
// its runs, schedule, settings and library manga (libraryManga replaces it when not 0). The
// re-link flag and the failures are cleared (Resumed is at, when the flag was set), the
// metadata bindings move to the new manga (a source the new manga has a binding of already
// keeps the watch's). A manga another watch has is ErrWatchExists.
func (s Watches) Relink(id int64, provider, mangaID, title string, libraryManga int64, at time.Time) error {
	err := s.DB.InTx(func(tx *sql.Tx) error {
		return relinkWatch(tx, id, provider, mangaID, title, libraryManga, at, true)
	})
	if err != nil {
		return fmt.Errorf("re-link watch %d to %s %q: %w", id, provider, mangaID, err)
	}
	return nil
}

// relinkWatch is Watches.Relink in tx, moveBindings moves the metadata bindings too.
func relinkWatch(tx *sql.Tx, id int64, provider, mangaID, title string, libraryManga int64, at time.Time, moveBindings bool) error {
	var oldProvider, oldManga string
	if err := tx.QueryRow(`SELECT provider, manga_id FROM watches WHERE id = ?`, id).Scan(&oldProvider, &oldManga); errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("watch %d: %w", id, ErrWatchNotFound)
	} else if err != nil {
		return err
	}
	var other int
	if err := tx.QueryRow(`SELECT count(*) FROM watches WHERE provider = ? AND manga_id = ? AND id != ?`, provider, mangaID, id).Scan(&other); err != nil {
		return err
	}
	if other != 0 {
		return fmt.Errorf("%s %q: %w", provider, mangaID, ErrWatchExists)
	}
	if _, err := tx.Exec(`UPDATE watches SET provider = ?, manga_id = ?, title = ?, library_manga = coalesce(?, library_manga),
	resumed = CASE WHEN relink != '' THEN ? ELSE resumed END, relink = '', failures = 0, last_error = '' WHERE id = ?`,
		provider, mangaID, title, nullID(libraryManga), unix(at), id); err != nil {
		return err
	}
	if !moveBindings || oldProvider == provider && oldManga == mangaID {
		return nil
	}
	if _, err := tx.Exec(`DELETE FROM metadata_bindings WHERE provider = ?3 AND manga_id = ?4 AND source IN
	(SELECT source FROM metadata_bindings WHERE provider = ?1 AND manga_id = ?2)`, oldProvider, oldManga, provider, mangaID); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE metadata_bindings SET provider = ?3, manga_id = ?4 WHERE provider = ?1 AND manga_id = ?2`,
		oldProvider, oldManga, provider, mangaID)
	return err
}

// WithDownloads are the watches whose checks downloaded chapters.
func (s Watches) WithDownloads() (map[int64]bool, error) {
	ids, err := queryAll(s.DB, func(rows *sql.Rows) (int64, error) {
		var id int64
		err := rows.Scan(&id)
		return id, err
	}, `SELECT DISTINCT r.watch FROM watch_runs r WHERE r.watch IS NOT NULL
	AND EXISTS (SELECT 1 FROM watch_run_chapters c WHERE c.watch_run = r.id AND c.path != '')`)
	if err != nil {
		return nil, fmt.Errorf("watches with downloads: %w", err)
	}
	with := make(map[int64]bool, len(ids))
	for _, id := range ids {
		with[id] = true
	}
	return with, nil
}

// LibraryNumbers are the chapter numbers the library manga has downloaded.
func (s Watches) LibraryNumbers(libraryManga int64) (map[float32]bool, error) {
	numbers, err := queryAll(s.DB, scanNumber, `SELECT c.number FROM library_chapters c
WHERE c.manga = ? AND EXISTS (SELECT 1 FROM downloads d WHERE d.chapter_id = c.id)`, libraryManga)
	if err != nil {
		return nil, fmt.Errorf("chapters of library manga %d: %w", libraryManga, err)
	}
	have := make(map[float32]bool, len(numbers))
	for _, n := range numbers {
		have[n] = true
	}
	return have, nil
}

// StartRun records the start of a check run, its ID is set.
func (s Watches) StartRun(run CheckRun) (CheckRun, error) {
	res, err := s.DB.Exec(`INSERT INTO check_runs (kind, started) VALUES (?, ?)`, run.Kind, unix(run.Started))
	if err != nil {
		return run, fmt.Errorf("start check run: %w", err)
	}
	run.ID, err = res.LastInsertId()
	return run, err
}

// FinishRun records the end of a check run and its totals.
func (s Watches) FinishRun(run CheckRun) error {
	_, err := s.DB.Exec(`UPDATE check_runs SET finished = ?, watches = ?, new_chapters = ?, errors = ? WHERE id = ?`,
		unix(run.Finished), run.Watches, run.NewChapters, run.Errors, run.ID)
	if err != nil {
		return fmt.Errorf("finish check run %d: %w", run.ID, err)
	}
	return nil
}

// RecordWatchRun records the check of a watch with its chapters, its ID is set.
func (s Watches) RecordWatchRun(r WatchRun) (WatchRun, error) {
	err := s.DB.InTx(func(tx *sql.Tx) error {
		r.NewChapters = r.New()
		res, err := tx.Exec(`INSERT INTO watch_runs (check_run, watch, provider, manga_id, title, from_number, status, error, started, finished, new_chapters)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, r.CheckRun, nullID(r.Watch), r.Provider, r.MangaID, r.Title, nullFloat(r.From), r.Status, r.Error,
			unix(r.Started), unix(r.Finished), r.NewChapters)
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
		err := rows.Scan(&r.ID, &r.CheckRun, &r.Watch, &r.Provider, &r.MangaID, &r.Title, &from, &r.Status, &r.Error, &started, &finished, &r.NewChapters)
		r.From, r.Started, r.Finished = floatPtr(from), fromUnix(started), fromUnix(finished)
		return r, err
	}, `SELECT id, check_run, coalesce(watch, 0), provider, manga_id, title, from_number, status, error, started, finished, new_chapters
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

// Lock is a lock held in the data database, so two processes don't do the same work at
// once (e.g. check the watches). It's kept alive with Heartbeat; a lock whose heartbeat is
// older than its staleness is taken over.
type Lock struct {
	Name string
	// Acquired is when it was taken.
	Acquired time.Time
	token    string
	db       *DB
}

// LockHolder is who holds a lock.
type LockHolder struct {
	// Name is the lock's, set by Holders.
	Name string
	// Purpose is what it's held for (LockAs), may be empty.
	Purpose   string
	PID       int
	Host      string
	Acquired  time.Time
	Heartbeat time.Time
}

// LockError is a lock held by someone else, it matches ErrLocked.
type LockError struct {
	Name   string
	Holder LockHolder
}

func (e *LockError) Error() string {
	return fmt.Sprintf("%s is locked by pid %d on %s since %s", e.Name, e.Holder.PID, e.Holder.Host, e.Holder.Acquired.Format(time.DateTime))
}

func (e *LockError) Is(target error) bool { return target == ErrLocked }

// Lock takes the named lock, a *LockError if someone else holds it and its heartbeat is
// newer than stale. A lock held by a process of this host that doesn't run anymore is
// taken over right away.
func (s Watches) Lock(name string, stale time.Duration) (*Lock, error) {
	return s.LockAs(name, "", stale)
}

// LockAs is Lock saying what the lock is held for (LockHolder.Purpose).
func (s Watches) LockAs(name, purpose string, stale time.Duration) (*Lock, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(b)
	host, _ := os.Hostname()
	now := time.Now()
	err := s.DB.InTx(func(tx *sql.Tx) error {
		var h LockHolder
		var acquired, heartbeat int64
		err := tx.QueryRow(`SELECT purpose, pid, host, acquired, heartbeat FROM locks WHERE name = ?`, name).Scan(&h.Purpose, &h.PID, &h.Host, &acquired, &heartbeat)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return err
		case h.Host == host && !proc.Alive(h.PID):
		case now.Sub(time.Unix(heartbeat, 0)) < stale:
			h.Acquired, h.Heartbeat = time.Unix(acquired, 0), time.Unix(heartbeat, 0)
			return &LockError{Name: name, Holder: h}
		}
		_, err = tx.Exec(`INSERT INTO locks (name, token, purpose, pid, host, acquired, heartbeat) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (name) DO UPDATE SET token = excluded.token, purpose = excluded.purpose, pid = excluded.pid, host = excluded.host,
	acquired = excluded.acquired, heartbeat = excluded.heartbeat`, name, token, purpose, os.Getpid(), host, now.Unix(), now.Unix())
		return err
	})
	if err != nil {
		if errors.Is(err, ErrLocked) {
			return nil, err
		}
		return nil, fmt.Errorf("lock %s: %w", name, err)
	}
	return &Lock{Name: name, Acquired: time.Unix(now.Unix(), 0), token: token, db: s.DB}, nil
}

// Holder is who holds the named lock, false when nobody does (or its heartbeat is older
// than stale).
func (s Watches) Holder(name string, stale time.Duration) (LockHolder, bool, error) {
	var h LockHolder
	var acquired, heartbeat int64
	err := s.DB.QueryRow(`SELECT purpose, pid, host, acquired, heartbeat FROM locks WHERE name = ?`, name).Scan(&h.Purpose, &h.PID, &h.Host, &acquired, &heartbeat)
	if errors.Is(err, sql.ErrNoRows) {
		return h, false, nil
	}
	if err != nil {
		return h, false, fmt.Errorf("lock %s: %w", name, err)
	}
	h.Acquired, h.Heartbeat = time.Unix(acquired, 0), time.Unix(heartbeat, 0)
	return h, time.Since(h.Heartbeat) < stale, nil
}

// Holders are the held locks whose name starts with prefix, by name: heartbeat newer than
// stale and, on this host, a holder that runs.
func (s Watches) Holders(prefix string, stale time.Duration) ([]LockHolder, error) {
	rows, err := s.DB.Query(`SELECT name, purpose, pid, host, acquired, heartbeat FROM locks WHERE substr(name, 1, length(?1)) = ?1 ORDER BY name`, prefix)
	if err != nil {
		return nil, fmt.Errorf("locks %s: %w", prefix, err)
	}
	defer rows.Close()
	host, _ := os.Hostname()
	var held []LockHolder
	for rows.Next() {
		var h LockHolder
		var acquired, heartbeat int64
		if err := rows.Scan(&h.Name, &h.Purpose, &h.PID, &h.Host, &acquired, &heartbeat); err != nil {
			return nil, fmt.Errorf("locks %s: %w", prefix, err)
		}
		h.Acquired, h.Heartbeat = time.Unix(acquired, 0), time.Unix(heartbeat, 0)
		if time.Since(h.Heartbeat) >= stale || (h.Host == host && !proc.Alive(h.PID)) {
			continue
		}
		held = append(held, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("locks %s: %w", prefix, err)
	}
	return held, nil
}

// Heartbeat keeps the lock alive, an error if it was lost (taken over as stale).
func (l *Lock) Heartbeat() error {
	res, err := l.db.Exec(`UPDATE locks SET heartbeat = ? WHERE name = ? AND token = ?`, time.Now().Unix(), l.Name, l.token)
	if err != nil {
		return fmt.Errorf("lock %s heartbeat: %w", l.Name, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("lock %s lost", l.Name)
	}
	return nil
}

// Unlock releases the lock, if still held.
func (l *Lock) Unlock() error {
	if _, err := l.db.Exec(`DELETE FROM locks WHERE name = ? AND token = ?`, l.Name, l.token); err != nil {
		return fmt.Errorf("unlock %s: %w", l.Name, err)
	}
	return nil
}

// relinkTable (migration 10): watches that need a re-link, with the reason; the
// placeholder ids (offline watches added by title) need one.
const relinkTable = `ALTER TABLE watches ADD COLUMN relink TEXT NOT NULL DEFAULT '';
UPDATE watches SET relink = 'placeholder manga id' WHERE manga_id LIKE 'title:%'`

// movedReason is the re-link reason of a watch moved from an old provider id: the site
// may have changed, and its manga ids with it.
func movedReason(oldID string) string {
	return "moved from " + oldID + ", the manga id may have changed"
}

// migrateWatches moves the watches and their runs of an old provider id to the new one, a
// manga watched under both keeps the new id's watch. The moved watches need a re-link
// (libmangal doesn't say whether the manga ids carried over): a check that finds the
// manga clears it.
func migrateWatches(tx *sql.Tx, oldID, newID string) error {
	if _, err := tx.Exec(`UPDATE watch_runs SET watch = (SELECT n.id FROM watches n JOIN watches o ON o.manga_id = n.manga_id
	WHERE o.id = watch_runs.watch AND n.provider = ?2)
WHERE watch IN (SELECT o.id FROM watches o JOIN watches n ON n.manga_id = o.manga_id AND n.provider = ?2 WHERE o.provider = ?1)`, oldID, newID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM watches WHERE provider = ?1 AND manga_id IN (SELECT manga_id FROM watches WHERE provider = ?2)`, oldID, newID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE watches SET provider = ?, relink = CASE WHEN relink = '' THEN ? ELSE relink END WHERE provider = ?`,
		newID, movedReason(oldID), oldID); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE watch_runs SET provider = ? WHERE provider = ?`, newID, oldID)
	return err
}

// watchRetry: the chapters a watch's last check failed to download.
const watchRetry = `ALTER TABLE watches ADD COLUMN retry TEXT NOT NULL DEFAULT ''`

// watchResumed: when a watch was last resumed or re-linked.
const watchResumed = `ALTER TABLE watches ADD COLUMN resumed INTEGER NOT NULL DEFAULT 0`

// formatNumbers stores chapter numbers as "1,2.5,3", parseNumbers reads them back.
func formatNumbers(ns []float32) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = strconv.FormatFloat(float64(n), 'f', -1, 32)
	}
	return strings.Join(parts, ",")
}

func parseNumbers(s string) []float32 {
	var ns []float32
	for part := range strings.SplitSeq(s, ",") {
		if n, err := strconv.ParseFloat(part, 32); err == nil {
			ns = append(ns, float32(n))
		}
	}
	return ns
}

// runNewChapters gives every watch run its count of new chapters (the existing ones: the
// chapters recorded under it).
const runNewChapters = `ALTER TABLE watch_runs ADD COLUMN new_chapters INTEGER NOT NULL DEFAULT 0;
UPDATE watch_runs SET new_chapters = (SELECT count(*) FROM watch_run_chapters c WHERE c.watch_run = watch_runs.id)`

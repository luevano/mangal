package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/luevano/mangal/v5/util/cron"
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
	// RelinkKind is RelinkProvider when the re-link was asked for because the manga needs
	// another provider (its provider lost the chapters), "" for a manga id that may be
	// wrong. A check that finds the manga keeps it.
	RelinkKind string `json:"relink_kind,omitempty"`
	// Hold is the watch's own hold mark (HoldHiatus, HoldEnded), for a watch without a
	// library manga: one with a library manga has the manga's (watch.Watch.Hold).
	Hold string `json:"own_hold,omitempty"`
	// Coverage is what its last check found missing on the provider (the library has
	// chapters the provider doesn't, see watch.Coverage), "" when nothing was.
	Coverage string `json:"coverage,omitempty"`
	// Retry are the chapters the last check failed to download: the next one tries them
	// again, also when they're below the last downloaded one.
	Retry []float32 `json:"retry,omitempty"`
	// Skip are chapter numbers never to download (fake or broken chapters), sorted.
	Skip []float32 `json:"skip,omitempty"`
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
	from_number, added, last_checked, last_gap_check, last_new, last_new_at, last_error, failures, relink, retry, resumed, skip,
	relink_kind, hold, coverage`

func scanWatch(rows *sql.Rows) (Watch, error) {
	var w Watch
	var added, checked, gapChecked, newAt, resumed int64
	var from, lastNew sql.NullFloat64
	var retry, skip string
	err := rows.Scan(&w.ID, &w.Provider, &w.MangaID, &w.Title, &w.LibraryManga, &w.Enabled, &w.Schedule, &w.Gaps,
		&from, &added, &checked, &gapChecked, &lastNew, &newAt, &w.LastError, &w.Failures, &w.RelinkReason, &retry, &resumed, &skip,
		&w.RelinkKind, &w.Hold, &w.Coverage)
	w.Retry, w.Skip = parseNumbers(retry), parseNumbers(skip)
	w.NeedsRelink = w.RelinkReason != ""
	w.From, w.LastNew = floatPtr(from), floatPtr(lastNew)
	w.Added, w.LastChecked, w.LastGapCheck, w.LastNewAt = fromUnix(added), fromUnix(checked), fromUnix(gapChecked), fromUnix(newAt)
	w.Resumed = fromUnix(resumed)
	return w, err
}

// Add adds a watch, its ID and Added are set. A manga already watched is ErrWatchExists.
func (s Watches) Add(w Watch) (Watch, error) {
	s.DB.touch()
	if w.Added.IsZero() {
		w.Added = time.Now()
	}
	// one transaction: two processes adding the same manga can't both see it missing
	err := s.DB.InTx(func(tx *sql.Tx) error {
		var exists int
		if err := tx.QueryRow(`SELECT count(*) FROM watches WHERE provider = ? AND manga_id = ?`, w.Provider, w.MangaID).Scan(&exists); err != nil {
			return err
		}
		if exists != 0 {
			return ErrWatchExists
		}
		res, err := tx.Exec(`INSERT INTO watches (provider, manga_id, title, library_manga, enabled, schedule, gaps, from_number, added, relink, skip, relink_kind, hold)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, w.Provider, w.MangaID, w.Title, nullID(w.LibraryManga), w.Enabled,
			w.Schedule, w.Gaps, nullFloat(w.From), unix(w.Added), w.relinkColumn(), formatNumbers(w.Skip), w.relinkKindColumn(), w.Hold)
		if err != nil {
			return err
		}
		w.ID, err = res.LastInsertId()
		return err
	})
	if err != nil {
		return w, fmt.Errorf("add watch of %s %q: %w", w.Provider, w.MangaID, err)
	}
	return w, nil
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
	return getWatch(s.DB, id)
}

func getWatch(q querier, id int64) (Watch, error) {
	watches, err := queryAll(q, scanWatch, `SELECT `+watchColumns+` FROM watches WHERE id = ?`, id)
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
	return s.DB.InTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`DELETE FROM watches WHERE id = ?`, id)
		if err != nil {
			return fmt.Errorf("remove watch %d: %w", id, err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("watch %d: %w", id, ErrWatchNotFound)
		}
		return forgetSubject(tx, fmt.Sprintf("watch/%d", id))
	})
}

// SetLibraryManga links the watch to its library manga, unless it's linked already.
func (s Watches) SetLibraryManga(id, manga int64) error {
	s.DB.touch()
	if _, err := s.DB.Exec(`UPDATE watches SET library_manga = ? WHERE id = ? AND library_manga IS NULL`, manga, id); err != nil {
		return fmt.Errorf("link watch %d to library manga %d: %w", id, manga, err)
	}
	return nil
}

// Save writes every field of the watch but its provider, manga id and added time.
func (s Watches) Save(w Watch) error {
	s.DB.touch()
	return saveWatch(s.DB, w)
}

// Update is Get, change and Save in one transaction, so writes made in between (a
// check's SaveCheck) aren't overwritten.
func (s Watches) Update(id int64, change func(*Watch)) (Watch, error) {
	s.DB.touch()
	var w Watch
	err := s.DB.InTx(func(tx *sql.Tx) error {
		var err error
		if w, err = getWatch(tx, id); err != nil {
			return err
		}
		change(&w)
		return saveWatch(tx, w)
	})
	return w, err
}

func saveWatch(e interface {
	Exec(string, ...any) (sql.Result, error)
}, w Watch) error {
	res, err := e.Exec(`UPDATE watches SET title = ?, library_manga = ?, enabled = ?, schedule = ?, gaps = ?,
	from_number = ?, last_checked = ?, last_gap_check = ?, last_new = ?, last_new_at = ?, last_error = ?, failures = ?, relink = ?, retry = ?, resumed = ?, skip = ?,
	relink_kind = ?, hold = ?, coverage = ?
WHERE id = ?`, w.Title, nullID(w.LibraryManga), w.Enabled, w.Schedule, w.Gaps,
		nullFloat(w.From), unix(w.LastChecked), unix(w.LastGapCheck), nullFloat(w.LastNew), unix(w.LastNewAt), w.LastError, w.Failures,
		w.relinkColumn(), formatNumbers(w.Retry), unix(w.Resumed), formatNumbers(w.Skip), w.relinkKindColumn(), w.Hold, w.Coverage, w.ID)
	if err != nil {
		return fmt.Errorf("save watch %d: %w", w.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("watch %d: %w", w.ID, ErrWatchNotFound)
	}
	return nil
}

// SaveCheck writes what a check of the watch changes (title, library manga when it had
// none, last checked/gap check/new, error, failures, re-link flag, retry, coverage), so edits made
// while it ran (paused, skip list, schedule) stay. A watch re-linked to another manga
// meanwhile is left as it is. The need of a new provider is the hold's (set or cleared
// while it ran, or when it started): the check leaves it.
func (s Watches) SaveCheck(w Watch) error {
	s.DB.touch()
	_, err := s.DB.Exec(`UPDATE watches SET title = ?, library_manga = coalesce(library_manga, ?), last_checked = ?, last_gap_check = ?,
	last_new = ?, last_new_at = ?, last_error = ?, failures = ?,
	relink = CASE WHEN relink_kind = 'provider' OR ? = 'provider' THEN relink ELSE ? END, retry = ?, coverage = ?
WHERE id = ? AND provider = ? AND manga_id = ?`, w.Title, nullID(w.LibraryManga), unix(w.LastChecked), unix(w.LastGapCheck),
		nullFloat(w.LastNew), unix(w.LastNewAt), w.LastError, w.Failures, w.relinkKindColumn(), w.relinkColumn(), formatNumbers(w.Retry),
		w.Coverage, w.ID, w.Provider, w.MangaID)
	if err != nil {
		return fmt.Errorf("save the check of watch %d: %w", w.ID, err)
	}
	return nil
}

// Relink points the watch to another manga (provider, manga id, title), keeping the rest:
// its runs, schedule, settings and library manga (libraryManga replaces it when not 0). The
// re-link flag and the failures are cleared (Resumed is at, when the flag was set), the
// metadata bindings move to the new manga (a source the new manga has a binding of already
// keeps the watch's). A manga another watch has is ErrWatchExists.
func (s Watches) Relink(id int64, provider, mangaID, title string, libraryManga int64, at time.Time) error {
	s.DB.touch()
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
	resumed = CASE WHEN relink != '' THEN ? ELSE resumed END, relink = '', relink_kind = '', coverage = '', failures = 0, last_error = '' WHERE id = ?`,
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

// watchSkip: the chapters a watch never downloads, and the ones a check left out for it.
const watchSkip = `ALTER TABLE watches ADD COLUMN skip TEXT NOT NULL DEFAULT '';
ALTER TABLE watch_runs ADD COLUMN skipped TEXT NOT NULL DEFAULT ''`

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

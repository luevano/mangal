package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// The journal records the file changes of the bulk library operations (rename, merge,
// move, migrate, mark, delete) as they're made, each operation with the snapshot of the
// data database taken before it, so it can be undone: its steps reversed and the
// database restored from the snapshot.
const journalTables = `CREATE TABLE journal (
	id          INTEGER PRIMARY KEY,
	kind        TEXT NOT NULL,
	summary     TEXT NOT NULL,
	started     INTEGER NOT NULL,
	finished    INTEGER NOT NULL DEFAULT 0,
	error       TEXT NOT NULL DEFAULT '',
	snapshot    TEXT NOT NULL,
	fingerprint TEXT NOT NULL DEFAULT '',
	undone      INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE journal_steps (
	op      INTEGER NOT NULL REFERENCES journal (id) ON DELETE CASCADE,
	seq     INTEGER NOT NULL,
	kind    TEXT NOT NULL,
	path    TEXT NOT NULL,
	dest    TEXT NOT NULL DEFAULT '',
	had_old INTEGER NOT NULL DEFAULT 0,
	old     BLOB,
	new     BLOB,
	PRIMARY KEY (op, seq)
) WITHOUT ROWID`

// keptTables aren't restored from a snapshot: no library operation changes them, and
// logins, searches or dismissed health findings made since are worth more than the old
// ones.
var keptTables = []string{
	TableTitleBindings, TableAuthTokens, TableAuthHistory, TableSearchHistory,
	"locks", "journal", "journal_steps", "health_ignores",
}

// StepKind is what a journal step did.
type StepKind string

const (
	// StepMove moved Path to Dest (a file or a folder).
	StepMove StepKind = "move"
	// StepMkdir created the folder Path.
	StepMkdir StepKind = "mkdir"
	// StepRmdir removed the empty folder Path.
	StepRmdir StepKind = "rmdir"
	// StepWrite wrote the file Path: Old is what it had (nil if it didn't exist), New what
	// was written.
	StepWrite StepKind = "write"
)

// Step is one file change of an operation, recorded before it's made.
type Step struct {
	Kind       StepKind
	Path, Dest string
	Old, New   []byte
}

// Op is a journaled operation.
type Op struct {
	ID      int64
	Kind    string
	Summary string
	Started time.Time
	// Finished is zero when it never finished (mangal was killed).
	Finished time.Time
	// Error is why it stopped midway, if it did.
	Error string
	// Snapshot is the copy of the data database taken before it.
	Snapshot string
	// Undone is when it was undone, zero if it wasn't.
	Undone time.Time
	// Steps is how many file changes it made.
	Steps int

	fingerprint string
	db          *DB
	seq         int
}

// Journal is the journal of the library operations, in the data database.
type Journal struct {
	DB *DB
}

// Journal is the journal of the library operations.
func (s Storage) Journal() Journal {
	return Journal{DB: s.Data}
}

// Begin starts journaling an operation, whose database snapshot is snapshot.
func (j Journal) Begin(kind, summary, snapshot string) (*Op, error) {
	op := &Op{Kind: kind, Summary: summary, Started: time.Now(), Snapshot: snapshot, db: j.DB}
	res, err := j.DB.Exec(`INSERT INTO journal (kind, summary, started, snapshot) VALUES (?, ?, ?, ?)`,
		kind, summary, op.Started.UnixMilli(), snapshot)
	if err != nil {
		return nil, fmt.Errorf("journal %s: %w", kind, err)
	}
	if op.ID, err = res.LastInsertId(); err != nil {
		return nil, fmt.Errorf("journal %s: %w", kind, err)
	}
	return op, nil
}

// Record adds the step, before it's made: a step that never happened is told apart when
// undoing (its target isn't there), one made but not recorded couldn't be undone.
func (o *Op) Record(s Step) error {
	o.seq++
	_, err := o.db.Exec(`INSERT INTO journal_steps (op, seq, kind, path, dest, had_old, old, new) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		o.ID, o.seq, string(s.Kind), s.Path, s.Dest, s.Old != nil, s.Old, s.New)
	if err != nil {
		return fmt.Errorf("journal %s %s: %w", s.Kind, s.Path, err)
	}
	o.Steps++
	return nil
}

// SetSummary replaces the summary with what was done after all (e.g. without the changes
// that were skipped), Finish stores it.
func (o *Op) SetSummary(summary string) { o.Summary = summary }

// Finish ends the operation, opErr is why it stopped (nil when it's done). It records
// what the database looks like now (an undo checks nothing changed since) and the summary.
func (o *Op) Finish(opErr error) error {
	sum, err := fingerprint(o.db)
	if err != nil {
		return fmt.Errorf("journal %s: %w", o.Kind, err)
	}
	msg := ""
	if opErr != nil {
		msg = opErr.Error()
	}
	o.Finished, o.Error, o.fingerprint = time.Now(), msg, sum
	_, err = o.db.Exec(`UPDATE journal SET finished = ?, error = ?, fingerprint = ?, summary = ? WHERE id = ?`,
		o.Finished.UnixMilli(), msg, sum, o.Summary, o.ID)
	if err != nil {
		return fmt.Errorf("journal %s: %w", o.Kind, err)
	}
	return nil
}

const opColumns = `id, kind, summary, started, finished, error, snapshot, fingerprint, undone,
	(SELECT count(*) FROM journal_steps WHERE op = journal.id)`

func scanOp(rows *sql.Rows) (Op, error) {
	var op Op
	var started, finished, undone int64
	err := rows.Scan(&op.ID, &op.Kind, &op.Summary, &started, &finished, &op.Error, &op.Snapshot, &op.fingerprint, &undone, &op.Steps)
	op.Started = time.UnixMilli(started)
	if finished != 0 {
		op.Finished = time.UnixMilli(finished)
	}
	if undone != 0 {
		op.Undone = time.UnixMilli(undone)
	}
	return op, err
}

// Ops are the journaled operations, newest first, at most limit (0 for all).
func (j Journal) Ops(limit int) ([]Op, error) {
	query := `SELECT ` + opColumns + ` FROM journal ORDER BY id DESC`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	return queryAll(j.DB, scanOp, query)
}

// Op is the operation with the id.
func (j Journal) Op(id int64) (Op, bool, error) {
	ops, err := queryAll(j.DB, scanOp, `SELECT `+opColumns+` FROM journal WHERE id = ?`, id)
	if err != nil || len(ops) == 0 {
		return Op{}, false, err
	}
	return ops[0], true, nil
}

// Last is the newest operation not undone yet.
func (j Journal) Last() (Op, bool, error) {
	ops, err := queryAll(j.DB, scanOp, `SELECT `+opColumns+` FROM journal WHERE undone = 0 ORDER BY id DESC LIMIT 1`)
	if err != nil || len(ops) == 0 {
		return Op{}, false, err
	}
	return ops[0], true, nil
}

// Newer are the operations after op not undone yet, newest first.
func (j Journal) Newer(op Op) ([]Op, error) {
	return queryAll(j.DB, scanOp, `SELECT `+opColumns+` FROM journal WHERE id > ? AND undone = 0 ORDER BY id DESC`, op.ID)
}

// Steps are the steps of the operation, in the order they were made.
func (j Journal) Steps(op Op) ([]Step, error) {
	return queryAll(j.DB, func(rows *sql.Rows) (Step, error) {
		var s Step
		var hadOld bool
		err := rows.Scan(&s.Kind, &s.Path, &s.Dest, &hadOld, &s.Old, &s.New)
		if hadOld && s.Old == nil {
			s.Old = []byte{}
		}
		return s, err
	}, `SELECT kind, path, dest, had_old, old, new FROM journal_steps WHERE op = ? ORDER BY seq`, op.ID)
}

// Forget drops the operations whose snapshot is one of these (deleted).
func (j Journal) Forget(snapshots []string) error {
	if len(snapshots) == 0 {
		return nil
	}
	return j.DB.InTx(func(tx *sql.Tx) error {
		for _, s := range snapshots {
			if _, err := tx.Exec(`DELETE FROM journal WHERE snapshot = ?`, s); err != nil {
				return fmt.Errorf("forget the operations of %s: %w", s, err)
			}
		}
		return nil
	})
}

// ErrNotFinished is an operation that never finished: what the database had at its end
// is unknown.
var ErrNotFinished = errors.New("the operation never finished (mangal was killed?), what changed since can't be told")

// Changed are the tables a restore of the operation's snapshot would change besides
// what the operation itself did: changed since it finished (downloads, reads, watch
// checks, other operations).
func (j Journal) Changed(op Op) ([]string, error) {
	if op.fingerprint == "" {
		return nil, ErrNotFinished
	}
	var then map[string]string
	if err := json.Unmarshal([]byte(op.fingerprint), &then); err != nil {
		return nil, fmt.Errorf("fingerprint of operation %d: %w", op.ID, err)
	}
	sum, err := fingerprint(j.DB)
	if err != nil {
		return nil, err
	}
	var now map[string]string
	if err := json.Unmarshal([]byte(sum), &now); err != nil {
		return nil, err
	}
	var changed []string
	for table, h := range now {
		if then[table] != h {
			changed = append(changed, table)
		}
	}
	for table := range then {
		if _, ok := now[table]; !ok {
			changed = append(changed, table)
		}
	}
	slices.Sort(changed)
	return changed, nil
}

// Restore puts the tables back as they were in the operation's snapshot (all but
// keptTables) and marks it undone, in one transaction.
func (j Journal) Restore(op Op) error {
	if _, err := os.Stat(op.Snapshot); err != nil {
		return fmt.Errorf("snapshot of operation %d: %w", op.ID, err)
	}
	ctx := context.Background()
	conn, err := j.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `ATTACH DATABASE ? AS snap`, op.Snapshot); err != nil {
		return fmt.Errorf("open snapshot %s: %w", op.Snapshot, err)
	}
	defer func() { _, _ = conn.ExecContext(ctx, `DETACH DATABASE snap`) }()

	var then, now int
	if err := conn.QueryRowContext(ctx, `PRAGMA snap.user_version`).Scan(&then); err != nil {
		return fmt.Errorf("read snapshot %s: %w", op.Snapshot, err)
	}
	if err := conn.QueryRowContext(ctx, `PRAGMA main.user_version`).Scan(&now); err != nil {
		return err
	}
	if then != now {
		return fmt.Errorf("snapshot %s has schema version %d, the database %d (another mangal version made it)", op.Snapshot, then, now)
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// deleting cascades only into tables that are restored too
	if _, err := tx.Exec(`PRAGMA defer_foreign_keys = ON`); err != nil {
		return err
	}
	tables, err := restorable(tx, "main")
	if err != nil {
		return err
	}
	snapTables, err := restorable(tx, "snap")
	if err != nil {
		return fmt.Errorf("read snapshot %s: %w", op.Snapshot, err)
	}
	if !slices.Equal(tables, snapTables) {
		return fmt.Errorf("snapshot %s has other tables than the database", op.Snapshot)
	}
	for _, t := range tables {
		if _, err := tx.Exec(`DELETE FROM main.` + quote(t)); err != nil {
			return fmt.Errorf("restore %s: %w", t, err)
		}
	}
	for _, t := range tables {
		cols, err := columns(tx, t)
		if err != nil {
			return fmt.Errorf("restore %s: %w", t, err)
		}
		list := strings.Join(cols, ", ")
		if _, err := tx.Exec(`INSERT INTO main.` + quote(t) + ` (` + list + `) SELECT ` + list + ` FROM snap.` + quote(t)); err != nil {
			return fmt.Errorf("restore %s: %w", t, err)
		}
	}
	if _, err := tx.Exec(`UPDATE journal SET undone = ? WHERE id = ?`, time.Now().UnixMilli(), op.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// MarkUndone marks the operation undone without restoring the database.
func (j Journal) MarkUndone(op Op) error {
	_, err := j.DB.Exec(`UPDATE journal SET undone = ? WHERE id = ?`, time.Now().UnixMilli(), op.ID)
	return err
}

func quote(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// restorable are the tables of the schema a snapshot restores, sorted.
func restorable(q querier, schema string) ([]string, error) {
	names, err := queryAll(q, func(rows *sql.Rows) (string, error) {
		var name string
		return name, rows.Scan(&name)
	}, `SELECT name FROM `+schema+`.sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(names, func(n string) bool { return slices.Contains(keptTables, n) }), nil
}

func columns(q querier, table string) ([]string, error) {
	cols, err := queryAll(q, func(rows *sql.Rows) (string, error) {
		var name string
		return name, rows.Scan(&name)
	}, `SELECT name FROM pragma_table_info(?, 'main') ORDER BY cid`, table)
	if err != nil {
		return nil, err
	}
	for i, c := range cols {
		cols[i] = quote(c)
	}
	return cols, nil
}

// fingerprint is a hash per restorable table of its rows, as JSON.
func fingerprint(db *DB) (string, error) {
	tables, err := restorable(db, "main")
	if err != nil {
		return "", err
	}
	sums := map[string]string{}
	for _, t := range tables {
		cols, err := columns(db, t)
		if err != nil {
			return "", err
		}
		order := make([]string, len(cols))
		for i := range cols {
			order[i] = fmt.Sprint(i + 1)
		}
		rows, err := db.Query(`SELECT * FROM main.` + quote(t) + ` ORDER BY ` + strings.Join(order, ", "))
		if err != nil {
			return "", fmt.Errorf("fingerprint %s: %w", t, err)
		}
		h := sha256.New()
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		for rows.Next() {
			if err := rows.Scan(ptrs...); err != nil {
				_ = rows.Close()
				return "", fmt.Errorf("fingerprint %s: %w", t, err)
			}
			for _, v := range values {
				fmt.Fprintf(h, "%T:%v\x00", v, v)
			}
			h.Write([]byte{'\n'})
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return "", fmt.Errorf("fingerprint %s: %w", t, err)
		}
		sums[t] = hex.EncodeToString(h.Sum(nil)[:16])
	}
	data, err := json.Marshal(sums)
	return string(data), err
}

// SnapshotPrefix starts the names of the snapshots: mangal-<time>-<operation>.sqlite.
const SnapshotPrefix = "mangal-"

// Snapshot copies the database into dir as mangal-<YYYYMMDD-HHMMSS>-<name>.sqlite
// (VACUUM INTO: a consistent copy, with what's still in the WAL, while other processes
// use it), only readable by the user.
func (db *DB) Snapshot(dir, name string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("snapshot: %w", err)
	}
	base := filepath.Join(dir, SnapshotPrefix+time.Now().Format("20060102-150405")+"-"+name)
	path := base + ".sqlite"
	// VACUUM INTO needs a missing or empty file: created empty first, with the mode
	var f *os.File
	var err error
	for i := 2; ; i++ {
		f, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if !errors.Is(err, fs.ErrExist) {
			break
		}
		path = fmt.Sprintf("%s-%d.sqlite", base, i)
	}
	if err != nil {
		return "", fmt.Errorf("snapshot: %w", err)
	}
	_ = f.Close()
	if _, err := db.Exec(`VACUUM INTO ?`, path); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("snapshot %s: %w", path, err)
	}
	return path, nil
}

// Snapshots are the snapshots in dir, oldest first.
func Snapshots(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	type snap struct {
		path string
		mod  time.Time
	}
	var snaps []snap
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, SnapshotPrefix) || filepath.Ext(name) != ".sqlite" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		snaps = append(snaps, snap{filepath.Join(dir, name), info.ModTime()})
	}
	// the names sort by time, but one made in the same second has a number
	stamp := func(path string) string {
		name := filepath.Base(path)
		return name[:min(len(name), len(SnapshotPrefix)+len("20060102-150405"))]
	}
	slices.SortStableFunc(snaps, func(a, b snap) int {
		if c := strings.Compare(stamp(a.path), stamp(b.path)); c != 0 {
			return c
		}
		return a.mod.Compare(b.mod)
	})
	paths := make([]string, len(snaps))
	for i, s := range snaps {
		paths[i] = s.path
	}
	return paths, nil
}

// PruneSnapshots deletes the oldest snapshots in dir past keep (at least 1), it returns
// the ones deleted.
func PruneSnapshots(dir string, keep int) ([]string, error) {
	keep = max(keep, 1)
	snaps, err := Snapshots(dir)
	if err != nil || len(snaps) <= keep {
		return nil, err
	}
	var removed []string
	for _, s := range snaps[:len(snaps)-keep] {
		if err := os.Remove(s); err != nil {
			return removed, err
		}
		removed = append(removed, s)
	}
	return removed, nil
}

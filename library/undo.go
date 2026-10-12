package library

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

// Undoer undoes journaled library operations: their file changes are reversed, newest
// first, and the data database is restored from the snapshot taken before them.
type Undoer struct {
	FS      afero.Fs
	Journal storage.Journal
	Modes   Modes
	// OnProgress, if set, is told where Apply is.
	OnProgress func(Progress)
}

// UndoAction is what undoing a step does.
type UndoAction string

const (
	UndoMoveBack  UndoAction = "move back"
	UndoRemoveDir UndoAction = "remove if empty"
	UndoMakeDir   UndoAction = "recreate"
	UndoRestore   UndoAction = "restore"
	UndoRemove    UndoAction = "remove"
	// UndoNothing: the step was never made or is undone already.
	UndoNothing UndoAction = "nothing to do"
	// UndoBlocked: undoing it would overwrite something or what it made is gone.
	UndoBlocked UndoAction = "blocked"
)

// UndoStep is a step of the operation and what undoing it does.
type UndoStep struct {
	Step   storage.Step
	Action UndoAction
	// Reason says why there's nothing to do or why it's blocked.
	Reason string
}

// UndoPlan is what undoing an operation does.
type UndoPlan struct {
	Op storage.Op
	// Steps are the operation's steps, newest first, as they're undone.
	Steps []UndoStep
	// Newer are operations made after it not undone yet: they go first.
	Newer []storage.Op
	// Changed are the tables changed since it finished (a restore loses those changes),
	// ChangedErr is set when that can't be told.
	Changed    []string
	ChangedErr error
	// SnapshotErr is set when the snapshot can't be restored (deleted).
	SnapshotErr error
}

// ErrNothingToUndo is an empty journal, or every operation in it undone.
var ErrNothingToUndo = errors.New("nothing to undo")

// Plan plans undoing the operation with the id, the last one not undone when id is 0.
func (u Undoer) Plan(id int64) (UndoPlan, error) {
	var op storage.Op
	var ok bool
	var err error
	if id == 0 {
		op, ok, err = u.Journal.Last()
		if err == nil && !ok {
			err = ErrNothingToUndo
		}
	} else {
		op, ok, err = u.Journal.Op(id)
		if err == nil && !ok {
			err = fmt.Errorf("no operation %d in the journal (\"mangal library undo --list\")", id)
		}
	}
	if err != nil {
		return UndoPlan{}, err
	}
	if !op.Undone.IsZero() {
		return UndoPlan{}, fmt.Errorf("operation %d was undone already (%s)", op.ID, op.Undone.Format(time.DateTime))
	}
	plan := UndoPlan{Op: op}
	if plan.Newer, err = u.Journal.Newer(op); err != nil {
		return plan, err
	}
	plan.Changed, plan.ChangedErr = u.Journal.Changed(op)
	if plan.ChangedErr != nil && !errors.Is(plan.ChangedErr, storage.ErrNotFinished) {
		return plan, plan.ChangedErr
	}
	if _, err := os.Stat(op.Snapshot); err != nil {
		plan.SnapshotErr = fmt.Errorf("its snapshot %s is gone", op.Snapshot)
	}

	steps, err := u.Journal.Steps(op)
	if err != nil {
		return plan, err
	}
	sim := &simFS{fs: u.FS}
	for i := len(steps) - 1; i >= 0; i-- {
		s := UndoStep{Step: steps[i]}
		s.Action, s.Reason = check(sim, steps[i])
		sim.apply(s)
		plan.Steps = append(plan.Steps, s)
	}
	return plan, nil
}

// Blocked are the steps that can't be undone.
func (p UndoPlan) Blocked() []UndoStep {
	var blocked []UndoStep
	for _, s := range p.Steps {
		if s.Action == UndoBlocked {
			blocked = append(blocked, s)
		}
	}
	return blocked
}

// Refusal is why the undo can't go ahead, nil when it can. force goes past what's only a
// risk of losing changes (changed since, blocked steps, an unfinished operation or a
// missing snapshot), never past newer operations.
func (p UndoPlan) Refusal(force bool) error {
	if len(p.Newer) != 0 {
		ids := make([]string, len(p.Newer))
		for i, op := range p.Newer {
			ids[i] = fmt.Sprint(op.ID)
		}
		return fmt.Errorf("operations made after it aren't undone (%s): undo them first, newest first", strings.Join(ids, ", "))
	}
	if force {
		return nil
	}
	var reasons []string
	if p.ChangedErr != nil {
		reasons = append(reasons, p.ChangedErr.Error())
	}
	if len(p.Changed) != 0 {
		reasons = append(reasons, fmt.Sprintf("the library changed since (%s): restoring the snapshot loses those changes", strings.Join(p.Changed, ", ")))
	}
	if n := len(p.Blocked()); n != 0 {
		reasons = append(reasons, fmt.Sprintf("%d steps can't be undone (listed)", n))
	}
	if p.SnapshotErr != nil {
		reasons = append(reasons, p.SnapshotErr.Error()+", the database can't be restored")
	}
	if len(reasons) == 0 {
		return nil
	}
	return errors.New(strings.Join(reasons, "; ") + "; --force undoes it anyway")
}

// Lines describe the undo, paths through rel (e.g. relative to the download directory).
func (p UndoPlan) Lines(rel func(string) string) []string {
	lines := []string{fmt.Sprintf("undo %d %s: %s (%s, %d steps)", p.Op.ID, p.Op.Kind, p.Op.Summary, p.Op.Started.Format(time.DateTime), p.Op.Steps)}
	if p.Op.Error != "" {
		lines = append(lines, "  it stopped midway: "+p.Op.Error)
	}
	for _, s := range p.Steps {
		lines = append(lines, "  "+s.Line(rel))
	}
	if p.SnapshotErr == nil {
		lines = append(lines, "  restore the database from "+p.Op.Snapshot)
	}
	return lines
}

// Line describes the undo of the step.
func (s UndoStep) Line(rel func(string) string) string {
	path := rel(s.Step.Path)
	switch s.Action {
	case UndoMoveBack:
		return fmt.Sprintf("move back %s -> %s", rel(s.Step.Dest), path)
	case UndoNothing, UndoBlocked:
		what := path
		if s.Step.Kind == storage.StepMove {
			what = fmt.Sprintf("%s -> %s", path, rel(s.Step.Dest))
		}
		return fmt.Sprintf("%s: %s %s: %s", s.Action, s.Step.Kind, what, s.Reason)
	}
	return fmt.Sprintf("%s %s", s.Action, path)
}

// existsFS answers what check needs, for the files as they are or as they'd be.
type existsFS interface {
	exists(path string) (bool, error)
	read(path string) ([]byte, error)
	sameFile(a, b string) bool
}

// check is what undoing the step does now.
func check(fsys existsFS, s storage.Step) (UndoAction, string) {
	switch s.Kind {
	case storage.StepMove:
		there, err := fsys.exists(s.Dest)
		if err != nil {
			return UndoBlocked, err.Error()
		}
		back, err := fsys.exists(s.Path)
		if err != nil {
			return UndoBlocked, err.Error()
		}
		switch {
		case there && back && fsys.sameFile(s.Dest, s.Path):
			// only the case changed
			return UndoMoveBack, ""
		case there && back:
			return UndoBlocked, "something is there again (never overwritten)"
		case there:
			return UndoMoveBack, ""
		case back:
			return UndoNothing, "not moved (never done or moved back)"
		}
		return UndoBlocked, "it's gone from there"
	case storage.StepMkdir:
		if exists, err := fsys.exists(s.Path); err != nil {
			return UndoBlocked, err.Error()
		} else if !exists {
			return UndoNothing, "gone already"
		}
		return UndoRemoveDir, ""
	case storage.StepRmdir:
		if exists, err := fsys.exists(s.Path); err != nil {
			return UndoBlocked, err.Error()
		} else if exists {
			return UndoNothing, "exists"
		}
		return UndoMakeDir, ""
	case storage.StepWrite:
		cur, err := fsys.read(s.Path)
		if err != nil {
			return UndoBlocked, err.Error()
		}
		switch {
		case cur != nil && sameJSON(cur, s.New):
			if s.Old == nil {
				return UndoRemove, ""
			}
			return UndoRestore, ""
		case cur == nil && s.Old == nil:
			return UndoNothing, "not there (never written, or removed or moved since)"
		case cur != nil && s.Old != nil && sameJSON(cur, s.Old):
			return UndoNothing, "as it was (never written or restored)"
		case cur == nil:
			return UndoBlocked, "gone"
		}
		return UndoBlocked, "changed since (e.g. rewritten by a download)"
	}
	return UndoBlocked, fmt.Sprintf("unknown step %q", s.Kind)
}

// sameJSON compares JSON files without their formatting, other files byte by byte.
func sameJSON(a, b []byte) bool {
	var ca, cb bytes.Buffer
	if json.Compact(&ca, a) != nil || json.Compact(&cb, b) != nil {
		return bytes.Equal(a, b)
	}
	return bytes.Equal(ca.Bytes(), cb.Bytes())
}

// realFS is the files as they are.
type realFS struct{ fs afero.Fs }

func (r realFS) exists(path string) (bool, error) { return afero.Exists(r.fs, path) }
func (r realFS) read(path string) ([]byte, error) { return readIfExists(r.fs, path) }
func (r realFS) sameFile(a, b string) bool        { return sameFile(r.fs, a, b) }

// simFS is the files as they'd be after the undo steps so far: each path is followed
// back through the simulated changes, newest first.
type simFS struct {
	fs      afero.Fs
	changes []UndoStep
}

func (s *simFS) apply(step UndoStep) {
	switch step.Action {
	case UndoMoveBack, UndoMakeDir, UndoRemove, UndoRestore:
		s.changes = append(s.changes, step)
	}
}

// under is true if path is dir or in it, rest is what's after dir.
func under(dir, path string) (string, bool) {
	if path == dir {
		return "", true
	}
	rest, ok := strings.CutPrefix(path, dir+string(filepath.Separator))
	return rest, ok
}

// resolve follows path back to where it really is now. gone is a path that won't exist,
// content a file's simulated content (nil: read the real file).
func (s *simFS) resolve(path string) (real string, gone bool, content []byte, known bool) {
	for i := len(s.changes) - 1; i >= 0; i-- {
		c := s.changes[i]
		switch c.Action {
		case UndoMoveBack:
			if rest, ok := under(c.Step.Path, path); ok {
				path = filepath.Join(c.Step.Dest, rest)
				continue
			}
			if _, ok := under(c.Step.Dest, path); ok {
				return path, true, nil, false
			}
		case UndoMakeDir:
			if path == c.Step.Path {
				return path, false, nil, true
			}
		case UndoRemove:
			if path == c.Step.Path {
				return path, true, nil, false
			}
		case UndoRestore:
			if path == c.Step.Path {
				return path, false, c.Step.Old, true
			}
		}
	}
	return path, false, nil, false
}

func (s *simFS) exists(path string) (bool, error) {
	real, gone, _, known := s.resolve(path)
	if gone {
		return false, nil
	}
	if known {
		return true, nil
	}
	return afero.Exists(s.fs, real)
}

func (s *simFS) read(path string) ([]byte, error) {
	real, gone, content, known := s.resolve(path)
	switch {
	case gone:
		return nil, nil
	case known && content != nil:
		return content, nil
	}
	return readIfExists(s.fs, real)
}

func (s *simFS) sameFile(a, b string) bool {
	ra, _, _, _ := s.resolve(a)
	rb, _, _, _ := s.resolve(b)
	return sameFile(s.fs, ra, rb)
}

// UndoResult is what Apply did.
type UndoResult struct {
	// Undone is how many steps were undone.
	Undone int
	// Skipped are the steps that couldn't be undone (with force), Kept the folders it
	// made that weren't empty.
	Skipped []UndoStep
	Kept    []string
	// Restored is whether the database was restored from the snapshot.
	Restored bool
}

// Apply undoes the plan when its Refusal(force) is nil: the steps, checked again as
// they're undone (nothing is overwritten, a step that can't be undone is skipped), then
// the database restore.
func (u Undoer) Apply(plan UndoPlan, force bool) (UndoResult, error) {
	var res UndoResult
	if err := plan.Refusal(force); err != nil {
		return res, err
	}
	fsys := realFS{fs: u.FS}
	for i, planned := range plan.Steps {
		s := planned.Step
		report(u.OnProgress, Progress{Phase: "undoing", Done: i + 1, Total: len(plan.Steps), Item: filepath.Base(s.Path)})
		action, reason := check(fsys, s)
		var err error
		switch action {
		case UndoNothing:
			continue
		case UndoBlocked:
			res.Skipped = append(res.Skipped, UndoStep{Step: s, Action: action, Reason: reason})
			continue
		case UndoMoveBack:
			err = moveDir(u.FS, nil, s.Dest, s.Path, u.Modes)
		case UndoRemoveDir:
			if empty, emptyErr := afero.IsEmpty(u.FS, s.Path); emptyErr != nil || !empty {
				res.Kept = append(res.Kept, s.Path)
				continue
			}
			err = u.FS.Remove(s.Path)
		case UndoMakeDir:
			err = u.FS.MkdirAll(s.Path, u.Modes.Dir)
		case UndoRemove:
			err = u.FS.Remove(s.Path)
		case UndoRestore:
			err = afero.WriteFile(u.FS, s.Path, s.Old, u.Modes.File)
		}
		if err != nil {
			return res, fmt.Errorf("undo %s %s: %w (partly undone, \"mangal library scan\" fixes the library)", s.Kind, s.Path, err)
		}
		res.Undone++
	}
	if plan.SnapshotErr != nil {
		if err := u.Journal.MarkUndone(plan.Op); err != nil {
			return res, err
		}
		return res, nil
	}
	if err := u.Journal.Restore(plan.Op); err != nil {
		return res, fmt.Errorf("restore the database: %w (the files are undone, \"mangal library scan\" fixes the library)", err)
	}
	res.Restored = true
	return res, nil
}

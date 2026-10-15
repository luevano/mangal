package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
)

// Kind is a kind of change.
type Kind string

// The kinds of change.
const (
	KindMerge        Kind = "merge"
	KindMove         Kind = "move"
	KindLink         Kind = "link"
	KindBackups      Kind = "backups"
	KindMark         Kind = "mark"
	KindRename       Kind = "rename"
	KindForget       Kind = "forget"
	KindDelete       Kind = "delete"
	KindEmptyTrash   Kind = "empty-trash"
	KindMigrate      Kind = "migrate"
	KindHistoryClear Kind = "history-clear"
	KindUndo         Kind = "undo"
	KindBind         Kind = "bind"
	KindUnbind       Kind = "unbind"
	KindWatch        Kind = "watch"
	KindResume       Kind = "resume"
)

// Kinds are every kind of change.
var Kinds = []Kind{KindMerge, KindMove, KindLink, KindBackups, KindMark, KindRename, KindForget, KindDelete,
	KindEmptyTrash, KindMigrate, KindHistoryClear, KindUndo, KindBind, KindUnbind, KindWatch, KindResume}

// Change is a typed request for a change (MergeReq, MoveReq, ...).
type Change interface {
	Kind() Kind
	plan(ctx context.Context, c *Core) (*prepared, error)
}

// Plan is what a change would do: its lines as the CLI lists them and a hash of them and
// of what they were planned from (the library's tables), so a stale plan is never applied.
type Plan struct {
	Kind Kind `json:"kind"`
	// Summary is the operation's, as the journal says it.
	Summary  string   `json:"summary,omitempty"`
	Lines    []string `json:"lines"`
	Warnings []string `json:"warnings,omitempty"`
	// Blocked are why it's refused; Force applies it anyway when Forceable.
	Blocked   []string `json:"blocked,omitempty"`
	Forceable bool     `json:"forceable,omitempty"`
	// Nothing is true when there's nothing to do.
	Nothing bool `json:"nothing,omitempty"`
	// Irreversible changes can't be undone (emptying the trash).
	Irreversible bool   `json:"irreversible,omitempty"`
	Hash         string `json:"hash"`
	// Change is the request planned.
	Change Change `json:"-"`
	// Detail is the kind's own plan (library.MergePlan, library.MovePlan, ...), for the
	// UIs that show more than the lines.
	Detail any `json:"-"`

	prep *prepared
	// fp is the fingerprint of the tables it was planned from
	fp string
}

// Line is a line of what an apply did, Warn for a skip or a failure.
type Line struct {
	Text string `json:"text"`
	Warn bool   `json:"warn,omitempty"`
}

// ApplyOpts are how a change is applied.
type ApplyOpts struct {
	// Expect is the hash of the plan that was confirmed: a plan that hashes otherwise now
	// is refused with a *PlanChangedError. Empty applies whatever the plan is now (Apply).
	Expect string
	// Force applies a Forceable plan with Blocked reasons.
	Force bool
	// NoWait gets an ErrBusy when the library's lock is held, instead of waiting for it
	// (OnWait, which may be nil, is told once).
	NoWait bool
	OnWait func(client.LibraryBusy)
	// OnLine, if set, is told each line as it's done.
	OnLine func(Line)
}

// OpResult is what an apply did.
type OpResult struct {
	Kind Kind `json:"kind"`
	// OpID is the journal's operation ("mangal library undo" undoes it), 0 when none.
	OpID int64 `json:"op,omitempty"`
	// Snapshot is the database snapshot taken before it, "" when none.
	Snapshot string `json:"snapshot,omitempty"`
	Summary  string `json:"summary,omitempty"`
	Lines    []Line `json:"lines,omitempty"`
	// Detail is the kind's own result (library.MarkResult, client.RenameResult, ...).
	Detail any `json:"-"`
}

// Changes plans and applies the destructive changes of the library.
type Changes interface {
	// Plan is what the change would do now.
	Plan(ctx context.Context, ch Change) (Plan, error)
	// Apply plans the change again holding the library's lock and applies it, refused
	// when opts.Expect is set and the plan's hash isn't it any more.
	Apply(ctx context.Context, ch Change, opts ApplyOpts) (OpResult, error)
	// ApplyPlan applies a plan: unless the library changed since it was made (then it's
	// planned again, refused when the new plan differs), the plan is applied as it is.
	ApplyPlan(ctx context.Context, p Plan, opts ApplyOpts) (OpResult, error)
}

var (
	// ErrPlanChanged is a plan that isn't what was confirmed any more (*PlanChangedError).
	ErrPlanChanged = errors.New("the plan changed")
	// ErrRefused is a plan with reasons not to apply it (*RefusedError).
	ErrRefused = errors.New("refused")
	// ErrNothingToDo is applying a plan with nothing to do.
	ErrNothingToDo = errors.New("nothing to do")
	// ErrBusy is the library's lock held by someone else (ApplyOpts.NoWait).
	ErrBusy = client.ErrLibraryBusy
	// ErrNotFound is a library manga, download or operation that isn't there.
	ErrNotFound = errors.New("not found")
	// ErrInvalid is an invalid request.
	ErrInvalid = errors.New("invalid")
)

// PlanChangedError is a plan that changed since it was confirmed, Plan is the new one.
type PlanChangedError struct {
	Plan Plan
}

func (e *PlanChangedError) Error() string {
	return "the library changed since the plan, look at it again"
}

func (e *PlanChangedError) Is(target error) bool { return target == ErrPlanChanged }

// RefusedError is a plan refused for its Reasons.
type RefusedError struct {
	Reasons []string
	err     error
}

func (e *RefusedError) Error() string {
	if e.err != nil {
		return e.err.Error()
	}
	return strings.Join(e.Reasons, "; ")
}

func (e *RefusedError) Is(target error) bool { return target == ErrRefused }

func (e *RefusedError) Unwrap() error { return e.err }

// NotFoundError is something that isn't there, it matches ErrNotFound.
type NotFoundError struct {
	What string
}

func (e *NotFoundError) Error() string { return e.What }

func (e *NotFoundError) Is(target error) bool { return target == ErrNotFound }

// invalid is an ErrInvalid with the message.
func invalid(format string, a ...any) error {
	return &invalidError{msg: fmt.Sprintf(format, a...)}
}

type invalidError struct{ msg string }

func (e *invalidError) Error() string        { return e.msg }
func (e *invalidError) Is(target error) bool { return target == ErrInvalid }

// prepared is a change planned: the plan, the tables it was planned from and how to
// apply it.
type prepared struct {
	plan Plan
	// tables are those the plan was made from: a change in them makes it stale
	tables []string
	// lock is false for changes outside the library (watches)
	nolock bool
	// refuse is why it can't be applied (with force), nil: the Blocked reasons
	refuse func(force bool) error
	apply  func(ctx context.Context, opts ApplyOpts, res *OpResult) error
}

// Tables a library change is planned from.
var (
	libraryTables = []string{"library_mangas", "library_sources", "library_chapters", "downloads", "metadata_bindings"}
	watchTables   = []string{"watches"}
	readTables    = []string{"reads"}
	journalTables = []string{"journal", "journal_steps"}
)

type changes struct {
	core *Core
}

func (c *changes) Plan(ctx context.Context, ch Change) (Plan, error) {
	p, err := c.prepare(ctx, ch)
	if err != nil {
		return Plan{}, err
	}
	return p.plan, nil
}

func (c *changes) prepare(ctx context.Context, ch Change) (*prepared, error) {
	if ch == nil {
		return nil, invalid("no change")
	}
	p, err := ch.plan(ctx, c.core)
	if err != nil {
		return nil, err
	}
	p.plan.Kind, p.plan.Change, p.plan.prep = ch.Kind(), ch, p
	if p.plan.Lines == nil {
		p.plan.Lines = []string{}
	}
	if p.plan.fp, err = c.core.env.Storage.Data.Fingerprint(p.tables...); err != nil {
		return nil, err
	}
	p.plan.Hash, err = hashPlan(p.plan)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// hashPlan hashes what the plan says and what it was planned from.
func hashPlan(p Plan) (string, error) {
	data, err := json.Marshal(struct {
		Kind         Kind
		Summary      string
		Lines        []string
		Warnings     []string
		Blocked      []string
		Nothing      bool
		Irreversible bool
		From         string
	}{p.Kind, p.Summary, p.Lines, p.Warnings, p.Blocked, p.Nothing, p.Irreversible, p.fp})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func (c *changes) Apply(ctx context.Context, ch Change, opts ApplyOpts) (OpResult, error) {
	if ch == nil {
		return OpResult{}, invalid("no change")
	}
	return c.apply(ctx, ch, nil, opts)
}

func (c *changes) ApplyPlan(ctx context.Context, p Plan, opts ApplyOpts) (OpResult, error) {
	if p.Change == nil {
		return OpResult{}, invalid("a plan without its change")
	}
	if opts.Expect == "" {
		opts.Expect = p.Hash
	}
	return c.apply(ctx, p.Change, p.prep, opts)
}

// apply locks the library, checks the plan (prep, nil to plan now) is still the one
// expected, and applies it.
func (c *changes) apply(ctx context.Context, ch Change, prep *prepared, opts ApplyOpts) (OpResult, error) {
	if prep == nil || !prep.nolock {
		var unlock func()
		var err error
		ctx, unlock, err = c.core.ops.Lock(ctx, string(ch.Kind()), !opts.NoWait, opts.OnWait)
		if err != nil {
			return OpResult{}, err
		}
		defer unlock()
	}
	if prep != nil {
		// the library as it was planned from: the plan holds
		fp, err := c.core.env.Storage.Data.Fingerprint(prep.tables...)
		if err != nil {
			return OpResult{}, err
		}
		if fp != prep.plan.fp {
			prep = nil
		}
	}
	if prep == nil {
		var err error
		if prep, err = c.prepare(ctx, ch); err != nil {
			return OpResult{}, err
		}
	}
	p := prep.plan
	if opts.Expect != "" && p.Hash != opts.Expect {
		return OpResult{}, &PlanChangedError{Plan: p}
	}
	if p.Nothing {
		return OpResult{}, ErrNothingToDo
	}
	if prep.refuse != nil {
		if err := prep.refuse(opts.Force); err != nil {
			return OpResult{}, &RefusedError{Reasons: p.Blocked, err: err}
		}
	} else if len(p.Blocked) != 0 && !(opts.Force && p.Forceable) {
		return OpResult{}, &RefusedError{Reasons: p.Blocked}
	}
	res := OpResult{Kind: p.Kind, Summary: p.Summary}
	err := prep.apply(ctx, opts, &res)
	return res, err
}

// journaled runs do as one library operation of the journal kind ("mangal library undo"
// undoes it), filling res's operation and snapshot.
func (c *Core) journaled(ctx context.Context, kind, summary string, res *OpResult, do func(library.Journal) error) error {
	info, err := c.ops.Run(ctx, kind, summary, func(j library.Journal) error {
		err := do(j)
		// the journal says what was done (SetSummary), so does the result
		if op, ok := j.(*storage.Op); ok {
			res.Summary = op.Summary
		}
		return err
	})
	res.OpID, res.Snapshot = info.ID, info.Snapshot
	return err
}

// line tells opts and res a line done.
func (o ApplyOpts) line(res *OpResult, text string, warn bool) {
	l := Line{Text: text, Warn: warn}
	res.Lines = append(res.Lines, l)
	if o.OnLine != nil {
		o.OnLine(l)
	}
}

type progressKey struct{}

// WithProgress is ctx telling onProgress where a plan or an apply is (scans, renames,
// lookups); libmangal's statuses go through progress.WithReporter.
func WithProgress(ctx context.Context, onProgress func(library.Progress)) context.Context {
	return context.WithValue(ctx, progressKey{}, onProgress)
}

// progressOf is the ctx's progress func, nil if none.
func progressOf(ctx context.Context) func(library.Progress) {
	f, _ := ctx.Value(progressKey{}).(func(library.Progress))
	return f
}

// report tells the ctx's progress func p, if any.
func report(ctx context.Context, p library.Progress) {
	if f := progressOf(ctx); f != nil {
		f(p)
	}
}

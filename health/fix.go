package health

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
)

// FixResult is what a finding's fix would do (the plan), or did.
type FixResult struct {
	Key  string `json:"key"`
	Kind string `json:"kind"`
	// Plan are the changes, one per line.
	Plan []string `json:"plan"`
	// Nothing is true when there's nothing to do any more.
	Nothing bool   `json:"nothing,omitempty"`
	Applied bool   `json:"applied"`
	Message string `json:"message,omitempty"`
	// Snapshot is the database snapshot of an applied library change: "mangal library
	// undo" reverts it.
	Snapshot string `json:"snapshot,omitempty"`
}

// NotFixableError is a fix the Fixer doesn't apply: its commands do (a merge asks which
// one stays, a scan has no plan, emptying the trash can't be undone).
type NotFixableError struct {
	Kind     string
	Commands []string
}

func (e *NotFixableError) Error() string {
	if e.Kind == "" {
		return "the finding has no fix to apply here: " + commandsOr(e.Commands)
	}
	return fmt.Sprintf("a %s isn't applied here: %s", e.Kind, commandsOr(e.Commands))
}

func commandsOr(commands []string) string {
	if len(commands) == 0 {
		return "nothing to run"
	}
	return "run " + strings.Join(commands, " or ")
}

// ErrChoice is a choice missing or not one of the fix's.
var ErrChoice = errors.New("invalid choice")

// Fixer plans and applies the fixes of findings (Fix.Action) as the TUI does, without
// asking: a plan first, the change only with apply. Library changes are one undoable
// operation each.
type Fixer struct {
	Watches watch.Manager
	// Linker links library mangas, its Files change the folders.
	Linker library.Linker
	// Operation runs a library change as one undoable operation (client.Operation).
	Operation func(kind, summary string, do func(library.Journal) error) (string, error)
	// Mark is how folders are marked.
	Mark library.MarkOptions
	// Renames is what library rename would do with the cached metadata, nil when renames
	// aren't fixed here.
	Renames func(ctx context.Context) ([]library.Rename, error)
	// OnProgress, if set, is told what an apply is doing.
	OnProgress func(string)
}

// Fix plans the finding's fix, and applies it with apply. choice is the id to link when
// the fix has several (one of Action.IDs, ErrChoice otherwise).
func (x Fixer) Fix(ctx context.Context, f Finding, choice string, apply bool) (FixResult, error) {
	a := f.Fix.Action
	if a == nil {
		return FixResult{}, &NotFixableError{Commands: f.Fix.Commands}
	}
	res := FixResult{Key: f.Key, Kind: a.Kind}
	var err error
	switch a.Kind {
	case ActionLink, ActionPick:
		err = x.link(ctx, a, choice, apply, &res)
	case ActionResume:
		err = x.resume(ctx, a, apply, &res)
	case ActionWatch:
		err = x.watch(ctx, f, a, apply, &res)
	case ActionMark:
		err = x.mark(ctx, a, apply, &res)
	case ActionForget:
		err = x.forget(a, apply, &res)
	case ActionRename:
		err = x.rename(ctx, a, apply, &res)
	default:
		return FixResult{}, &NotFixableError{Kind: a.Kind, Commands: f.Fix.Commands}
	}
	if err != nil {
		return FixResult{}, err
	}
	res.Applied = apply && !res.Nothing
	return res, nil
}

func (x Fixer) progress(msg string) {
	if x.OnProgress != nil {
		x.OnProgress(msg)
	}
}

func (x Fixer) manga(id int64, res *FixResult) (storage.Manga, bool, error) {
	m, ok, err := x.Linker.Library.MangaByID(id)
	if err == nil && !ok {
		res.Nothing, res.Message = true, fmt.Sprintf("library manga %d is gone", id)
	}
	return m, ok, err
}

func (x Fixer) link(ctx context.Context, a *Action, choice string, apply bool, res *FixResult) error {
	id := choice
	switch {
	case id == "" && len(a.IDs) == 1:
		id = a.IDs[0]
	case id == "":
		return fmt.Errorf("%w: choose one of %s", ErrChoice, strings.Join(a.IDs, ", "))
	case !slices.Contains(a.IDs, id):
		return fmt.Errorf("%w: %s isn't one of %s", ErrChoice, id, strings.Join(a.IDs, ", "))
	}
	m, ok, err := x.manga(a.Manga, res)
	if err != nil || !ok {
		return err
	}
	plan, err := x.Linker.PlanLinks(ctx, m, library.Links{a.Source: id}, library.PlanOptions{})
	if err != nil {
		return err
	}
	res.Plan = plan.Lines()
	if len(plan.Changes) == 0 {
		res.Nothing, res.Message = true, "nothing to change"
		return nil
	}
	if !apply {
		return nil
	}
	res.Snapshot, err = x.Operation("link", fmt.Sprintf("link %d %q", m.ID, m.Title), func(j library.Journal) error {
		l := x.Linker
		l.Journal = j
		return l.ApplyLinks(plan, library.LinkOptions{})
	})
	if err != nil {
		return err
	}
	res.Message = fmt.Sprintf("linked %s to %s %s", m.Title, sourceLabel(a), id)
	return nil
}

func sourceLabel(a *Action) string {
	if name, ok := linkNames[a.Source]; ok {
		return name
	}
	return string(a.Source)
}

func (x Fixer) resume(ctx context.Context, a *Action, apply bool, res *FixResult) error {
	w, err := x.Watches.Get(ctx, a.Watch)
	if err != nil {
		return err
	}
	if w.Enabled {
		res.Nothing, res.Message = true, fmt.Sprintf("watch %d is enabled", w.ID)
		return nil
	}
	res.Plan = []string{fmt.Sprintf("resume watch %d %s (%s)", w.ID, w.Title, w.Provider)}
	if !apply {
		return nil
	}
	if _, err := x.Watches.SetEnabled(ctx, w.ID, true); err != nil {
		return err
	}
	res.Message = "resumed " + w.Title
	return nil
}

func (x Fixer) watch(ctx context.Context, f Finding, a *Action, apply bool, res *FixResult) error {
	res.Plan = []string{fmt.Sprintf("watch %s on %s (mid:%s): its first check downloads the chapters after the library's last", f.Subject.Name, a.Provider, a.MangaID)}
	if !apply {
		return nil
	}
	w, err := x.Watches.Add(ctx, watch.AddOptions{Provider: a.Provider, MangaID: a.MangaID})
	if err != nil {
		return err
	}
	res.Message = fmt.Sprintf("watching %s on %s (watch %d)", w.Title, w.Provider, w.ID)
	return nil
}

func (x Fixer) mark(ctx context.Context, a *Action, apply bool, res *FixResult) error {
	ids := a.Mangas
	if a.Manga != 0 {
		ids = []int64{a.Manga}
	}
	files := x.Linker.Files
	dry := x.Mark
	dry.DryRun = true
	var mangas []storage.Manga
	for _, id := range ids {
		m, ok, err := files.Library.MangaByID(id)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if _, do, err := files.MarkManga(m, dry); err != nil {
			return err
		} else if do {
			mangas = append(mangas, m)
			res.Plan = append(res.Plan, "write "+filepath.Join(m.Path(), libmangal.FilenameMangaDirInfo))
		}
	}
	if len(mangas) == 0 {
		res.Nothing, res.Message = true, "nothing to mark any more"
		return nil
	}
	if !apply {
		return nil
	}
	var err error
	res.Snapshot, err = x.Operation("mark", fmt.Sprintf("mark %d folders", len(mangas)), func(j library.Journal) error {
		files.Journal = j
		for i, m := range mangas {
			if err := ctx.Err(); err != nil {
				return err
			}
			x.progress(fmt.Sprintf("marking: %d/%d (%s)", i+1, len(mangas), m.Title))
			if _, _, err := files.MarkManga(m, x.Mark); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	res.Message = fmt.Sprintf("marked %s", plural(len(mangas), "folder"))
	return nil
}

func (x Fixer) forget(a *Action, apply bool, res *FixResult) error {
	m, ok, err := x.manga(a.Manga, res)
	if err != nil || !ok {
		return err
	}
	res.Plan = []string{fmt.Sprintf("forget library manga %d %s (%s), its files aren't touched", m.ID, m.Title, m.Path())}
	if !apply {
		return nil
	}
	res.Snapshot, err = x.Operation("remove", fmt.Sprintf("remove missing %d %q", m.ID, m.Title), func(library.Journal) error {
		return x.Linker.Library.RemoveMangas(m.ID)
	})
	if err != nil {
		return err
	}
	res.Message = "forgot " + m.Title
	return nil
}

func (x Fixer) rename(ctx context.Context, a *Action, apply bool, res *FixResult) error {
	if x.Renames == nil {
		return &NotFixableError{Kind: a.Kind, Commands: []string{"mangal library rename"}}
	}
	all, err := x.Renames(ctx)
	if err != nil {
		return err
	}
	var renames []library.Rename
	for _, r := range all {
		if !slices.Contains(a.Paths, r.From) && !slices.Contains(a.Paths, r.ProviderDir) {
			continue
		}
		renames = append(renames, r)
		switch {
		case r.Skip != "":
			res.Plan = append(res.Plan, fmt.Sprintf("skip %s: %s", r.From, r.Skip))
		case r.RemoveEmpty:
			res.Plan = append(res.Plan, fmt.Sprintf("remove %s once empty", r.From))
		default:
			res.Plan = append(res.Plan, fmt.Sprintf("%s -> %s", r.From, r.To))
		}
	}
	if !slices.ContainsFunc(renames, func(r library.Rename) bool { return r.Skip == "" }) {
		res.Nothing, res.Message = true, "nothing to rename any more"
		return nil
	}
	if !apply {
		return nil
	}
	files := x.Linker.Files
	done, skipped := 0, 0
	res.Snapshot, err = x.Operation("rename", fmt.Sprintf("rename %d folders", len(renames)), func(j library.Journal) error {
		return library.ApplyRenamesContext(ctx, files.FS, files.Modes, files.Library, j, renames, func(p library.Progress) { x.progress(p.String()) }, func(i int, err error) {
			switch {
			case errors.Is(err, library.ErrNotEmpty):
			case err != nil:
				skipped++
			case !renames[i].RemoveEmpty:
				done++
			}
		})
	})
	if err != nil {
		return err
	}
	res.Message = fmt.Sprintf("renamed %s", plural(done, "folder"))
	if skipped != 0 {
		res.Message += fmt.Sprintf(", %d skipped (mangal library rename says why)", skipped)
	}
	return nil
}

// Find is the finding (or summary item) with the key.
func (r Report) Find(key string) (Finding, bool) {
	return findKey(r.Findings, key)
}

func findKey(findings []Finding, key string) (Finding, bool) {
	for _, f := range findings {
		if strings.EqualFold(f.Key, key) {
			return f, true
		}
		if item, ok := findKey(f.Items, key); ok {
			return item, true
		}
	}
	return Finding{}, false
}

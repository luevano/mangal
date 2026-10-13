package library

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

// MergePlan is what merging other into keep does.
type MergePlan struct {
	Keep, Other storage.Manga
	// Moves are other's chapters keep doesn't have, moved into keep's folder.
	Moves []storage.MergeMove
	// Trashed are other's chapters keep already has (same number and format).
	Trashed []string
}

// PlanMerge plans merging the manga other into keep: keep gets every chapter, keeping
// its own file when both have it.
func (f Files) PlanMerge(keep, other storage.Manga) (MergePlan, error) {
	plan := MergePlan{Keep: keep, Other: other}
	if keep.ID == other.ID || keep.Path() == other.Path() {
		return plan, errors.New("can't merge a manga with itself")
	}
	if keep.Dir == "" {
		return plan, fmt.Errorf("manga %q has no folder of its own to merge into", keep.Title)
	}
	// what's left of other's folder goes to the trash
	if other.Dir != "" && isInside(other.Path(), keep.Path()) {
		return plan, fmt.Errorf("the folder of %q is inside the folder of %q", keep.Title, other.Title)
	}
	keepDownloads, err := f.Library.Downloads(keep.ID)
	if err != nil {
		return plan, err
	}
	type key struct {
		number float32
		format string
	}
	have := map[key]bool{}
	for _, dl := range keepDownloads {
		have[key{dl.Chapter, dl.Format}] = true
	}

	otherDownloads, err := f.Library.Downloads(other.ID)
	if err != nil {
		return plan, err
	}
	taken := map[string]bool{}
	for _, dl := range otherDownloads {
		if have[key{dl.Chapter, dl.Format}] {
			plan.Trashed = append(plan.Trashed, dl.Path)
			continue
		}
		have[key{dl.Chapter, dl.Format}] = true

		rel, err := filepath.Rel(other.Path(), dl.Path)
		if err != nil || !isInside(other.Path(), dl.Path) {
			rel = filepath.Base(dl.Path)
		}
		// a folder chapter ("[0001.0] Chapter 1") has no extension
		isDir, err := afero.DirExists(f.FS, dl.Path)
		if err != nil {
			return plan, err
		}
		to, err := freeName(f.FS, filepath.Join(keep.Path(), rel), isDir, taken)
		if err != nil {
			return plan, err
		}
		taken[to] = true
		plan.Moves = append(plan.Moves, storage.MergeMove{From: dl.Path, To: to})
	}
	return plan, nil
}

// unplanned refuses other's downloads the plan doesn't have (downloaded since it was
// made): merging the library mangas would forget them.
func (f Files) unplanned(plan MergePlan) error {
	downloads, err := f.Library.Downloads(plan.Other.ID)
	if err != nil {
		return err
	}
	var paths []string
	for _, dl := range downloads {
		if !slices.Contains(plan.Trashed, dl.Path) && !slices.ContainsFunc(plan.Moves, func(m storage.MergeMove) bool { return m.From == dl.Path }) {
			paths = append(paths, filepath.Base(dl.Path))
		}
	}
	if len(paths) != 0 {
		return fmt.Errorf("%q got downloads since the plan (%s): plan the merge again", plan.Other.Title, strings.Join(paths, ", "))
	}
	return nil
}

// isInside is true if path is dir or in it.
func isInside(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// ApplyMerge does the plan: moves and trashes the files, updates the library and
// trashes what's left of other's folder (its .libmangal.json, cover, ...).
//
// Unlike ApplyMove and ApplyLinks (one folder, one transaction: a failed step rolls back
// what was done), a merge moves many files and doesn't roll back: the library is updated
// as each file moves, so if it stops midway the library still matches the files. Journaled
// (Journal set), the undo reverts it; otherwise "mangal library scan" finishes the job.
func (f Files) ApplyMerge(plan MergePlan) error {
	return f.ApplyMergeContext(context.Background(), plan)
}

// ApplyMergeContext is ApplyMerge, stopped between files when ctx is done.
func (f Files) ApplyMergeContext(ctx context.Context, plan MergePlan) error {
	started := false
	fail := func(err error) error {
		if !started {
			return fmt.Errorf("merge %q into %q: %w", plan.Other.Title, plan.Keep.Title, err)
		}
		hint := `"mangal library scan" fixes the rest`
		if f.Journal != nil {
			hint = "undoing it reverts it"
		}
		return fmt.Errorf("merge %q into %q: %w (it's partly done, %s)", plan.Other.Title, plan.Keep.Title, err, hint)
	}

	// a download of other's since the plan would go with its rows
	if err := f.unplanned(plan); err != nil {
		return fail(err)
	}
	batch := newTrashBatch()
	total, done := len(plan.Trashed)+len(plan.Moves), 0
	step := func(path string) error {
		done++
		report(f.OnProgress, Progress{Phase: "merging", Done: done, Total: total, Item: filepath.Base(path)})
		return ctx.Err()
	}
	for _, path := range plan.Trashed {
		if err := step(path); err != nil {
			return fail(err)
		}
		if err := moveToTrash(f.FS, f.Journal, f.Root, batch, path, f.Modes); err != nil {
			return fail(fmt.Errorf("trash %s: %w", path, err))
		}
		started = true
		if err := f.Library.RemoveDownload(path); err != nil {
			return fail(err)
		}
	}
	for _, m := range plan.Moves {
		if err := step(m.From); err != nil {
			return fail(err)
		}
		if err := moveFile(f.FS, f.Journal, m.From, m.To, f.Modes); err != nil {
			return fail(fmt.Errorf("move %s: %w", m.From, err))
		}
		started = true
		if err := f.Library.MergeDownload(plan.Keep.ID, m); err != nil {
			return fail(err)
		}
	}
	if err := f.unplanned(plan); err != nil {
		return fail(err)
	}
	if err := f.Library.Merge(plan.Keep.ID, plan.Other.ID, nil, nil); err != nil {
		return fail(err)
	}
	started = true
	if plan.Other.Dir == "" {
		return nil
	}
	if exists, err := afero.DirExists(f.FS, plan.Other.Path()); err != nil || !exists {
		if err != nil {
			return fail(err)
		}
		return nil
	}
	// one by one, the batch may already have some of the folder's files
	entries, err := afero.ReadDir(f.FS, plan.Other.Path())
	if err != nil {
		return fail(err)
	}
	for _, e := range entries {
		path := filepath.Join(plan.Other.Path(), e.Name())
		if err := moveToTrash(f.FS, f.Journal, f.Root, batch, path, f.Modes); err != nil {
			return fail(fmt.Errorf("trash %s: %w", path, err))
		}
	}
	if err := removeDir(f.FS, f.Journal, plan.Other.Path()); err != nil {
		return fail(err)
	}
	return nil
}

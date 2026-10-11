package library

import (
	"errors"
	"fmt"
	"path/filepath"
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
func PlanMerge(fsys afero.Fs, library storage.Library, keep, other storage.Manga) (MergePlan, error) {
	plan := MergePlan{Keep: keep, Other: other}
	if keep.ID == other.ID || keep.Path() == other.Path() {
		return plan, errors.New("can't merge a manga with itself")
	}
	if keep.Dir == "" {
		return plan, fmt.Errorf("manga %q has no folder of its own to merge into", keep.Title)
	}
	keepDownloads, err := library.Downloads(keep.ID)
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

	otherDownloads, err := library.Downloads(other.ID)
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
		if err != nil || strings.HasPrefix(rel, "..") {
			rel = filepath.Base(dl.Path)
		}
		to := filepath.Join(keep.Path(), rel)
		for i := 2; ; i++ {
			exists, err := afero.Exists(fsys, to)
			if err != nil {
				return plan, err
			}
			if !exists && !taken[to] {
				break
			}
			ext := filepath.Ext(rel)
			to = filepath.Join(keep.Path(), fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(rel, ext), i, ext))
		}
		taken[to] = true
		plan.Moves = append(plan.Moves, storage.MergeMove{From: dl.Path, To: to})
	}
	return plan, nil
}

// ApplyMerge does the plan: moves and trashes the files, updates the library and
// trashes what's left of other's folder (its .libmangal.json, cover, ...).
func ApplyMerge(fsys afero.Fs, library storage.Library, root string, plan MergePlan) error {
	batch := newTrashBatch()
	for _, path := range plan.Trashed {
		if err := moveToTrash(fsys, root, batch, path); err != nil {
			return fmt.Errorf("trash %s: %w", path, err)
		}
	}
	for _, m := range plan.Moves {
		if err := fsys.MkdirAll(filepath.Dir(m.To), 0o755); err != nil {
			return err
		}
		if err := fsys.Rename(m.From, m.To); err != nil {
			return fmt.Errorf("move %s: %w", m.From, err)
		}
	}
	if err := library.Merge(plan.Keep.ID, plan.Other.ID, plan.Moves, plan.Trashed); err != nil {
		return err
	}
	if plan.Other.Dir == "" {
		return nil
	}
	if exists, err := afero.DirExists(fsys, plan.Other.Path()); err != nil || !exists {
		return err
	}
	// one by one, the batch may already have some of the folder's files
	entries, err := afero.ReadDir(fsys, plan.Other.Path())
	if err != nil {
		return err
	}
	for _, e := range entries {
		path := filepath.Join(plan.Other.Path(), e.Name())
		if err := moveToTrash(fsys, root, batch, path); err != nil {
			return fmt.Errorf("trash %s: %w", path, err)
		}
	}
	return fsys.Remove(plan.Other.Path())
}

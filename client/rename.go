package client

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/luevano/mangal/v5/library"
)

// RenamePlan is what library rename would do.
type RenamePlan struct {
	// Root is the download directory.
	Root string
	// Renames are the chapter files' (they use the paths of now), then the folders', with
	// the ones skipped.
	Renames []library.Rename
	// Disagree are the folders whose ids disagree with their links (the links are used):
	// "folder: line", the folder relative to Root.
	Disagree []string
	// Unmarked is how many folders have no .libmangal.json: they aren't renamed.
	Unmarked int
	// Untitled is how many chapter files are kept: no chapter title recorded.
	Untitled int
}

// Rel is p relative to the download directory.
func (p RenamePlan) Rel(path string) string {
	if r, err := filepath.Rel(p.Root, path); err == nil {
		return r
	}
	return path
}

// NoMetadata are the folders skipped for their metadata not being found (Fallback
// renames them).
func (p RenamePlan) NoMetadata() []library.Rename {
	var skipped []library.Rename
	for _, r := range p.Renames {
		if r.NoMetadata && r.Skip != "" {
			skipped = append(skipped, r)
		}
	}
	return skipped
}

// Line is the rename as the plan lists it: "from -> to", "skip from: why", "remove from
// once empty"; a chapter file's new name only.
func (p RenamePlan) Line(r library.Rename) string {
	switch {
	case r.Skip != "":
		return fmt.Sprintf("skip %s: %s", p.Rel(r.From), r.Skip)
	case r.RemoveEmpty:
		return fmt.Sprintf("remove %s once empty", p.Rel(r.From))
	case r.Chapter:
		return fmt.Sprintf("%s -> %s", p.Rel(r.From), filepath.Base(r.To))
	}
	return fmt.Sprintf("%s -> %s", p.Rel(r.From), p.Rel(r.To))
}

// RenameCounts counts the folder and chapter file renames that aren't skipped.
func RenameCounts(renames []library.Rename) (folders, files int) {
	for _, r := range renames {
		switch {
		case r.Skip != "" || r.RemoveEmpty:
		case r.Chapter:
			files++
		default:
			folders++
		}
	}
	return folders, files
}

// RenameSummary is the journal's summary of a rename.
func RenameSummary(folders, files int) string {
	var what []string
	if folders != 0 || files == 0 {
		what = append(what, fmt.Sprintf("%d folders", folders))
	}
	if files != 0 {
		what = append(what, fmt.Sprintf("%d chapter files", files))
	}
	return "rename " + strings.Join(what, ", ")
}

// RenameResult is what a library rename did.
type RenameResult struct {
	Folders, Files int
	// Kept are the directories left as they aren't empty, Failed the renames that failed.
	Kept, Failed int
	// Snapshot is the database snapshot: "mangal library undo" reverts the rename.
	Snapshot string
}

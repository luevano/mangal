package client

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/afs"
)

// RenameOptions are library rename's options.
type RenameOptions struct {
	// Refresh gets the AniList metadata again instead of the cached one.
	Refresh bool
	// Chapters renames the chapter files to the chapter name template too.
	Chapters bool
	// Fallback renames the folders whose metadata isn't found to the fallback name.
	Fallback bool
	// OnProgress, if set, is told where the plan is.
	OnProgress func(library.Progress)
}

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

// PlanLibraryRename plans library rename of the download directory, with the AniList
// metadata (looked up when not cached, again with opts.Refresh).
func PlanLibraryRename(ctx context.Context, lib storage.Library, opts RenameOptions) (RenamePlan, error) {
	dir, err := path.ExistingDownloads()
	if err != nil {
		return RenamePlan{}, err
	}
	plan := RenamePlan{Root: dir}
	namer, err := LibraryNamer(lib, anilist.MetadataByIDsBatch)
	if err != nil {
		return RenamePlan{}, err
	}
	namer.OnProgress = opts.OnProgress
	namer.OnDisagree = func(folder string, d []string) {
		for _, line := range d {
			plan.Disagree = append(plan.Disagree, fmt.Sprintf("%s: %s", plan.Rel(folder), line))
		}
	}
	if opts.Refresh {
		namer.Metadata = anilist.MetadataByIDsFresh
	}
	namer.Fallback = opts.Fallback
	providerDirs := config.Download.Provider.CreateDir.Get()
	renames, err := namer.Plan(ctx, dir, providerDirs)
	if err != nil {
		return RenamePlan{}, err
	}
	if opts.Chapters {
		mangas, err := lib.Mangas()
		if err != nil {
			return RenamePlan{}, err
		}
		chapters, err := namer.PlanChapters(mangas)
		if err != nil {
			return RenamePlan{}, err
		}
		var kept []library.Rename
		for _, r := range chapters {
			if r.Skip == "no chapter title recorded" {
				plan.Untitled++
				continue
			}
			kept = append(kept, r)
		}
		// before the folders: they use the paths of now
		renames = append(kept, renames...)
	}
	plan.Renames = renames
	if unmarked, err := library.Unmarked(afs.Afero, dir, providerDirs); err == nil {
		plan.Unmarked = len(unmarked)
	}
	return plan, nil
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

// RenameResult is what ApplyLibraryRename did.
type RenameResult struct {
	Folders, Files int
	// Kept are the directories left as they aren't empty, Failed the renames that failed.
	Kept, Failed int
	// Snapshot is the database snapshot: "mangal library undo" reverts the rename.
	Snapshot string
}

// ApplyLibraryRename does the renames as one operation ("mangal library undo" reverts
// it), stopped between renames when ctx is done. done (may be nil) is told each rename's
// error (nil when done; library.ErrNotEmpty for a directory left in place).
func ApplyLibraryRename(ctx context.Context, lib storage.Library, renames []library.Rename, onProgress func(library.Progress), done func(i int, err error)) (RenameResult, error) {
	var res RenameResult
	snapshot, err := Operation("rename", RenameSummary(RenameCounts(renames)), func(j library.Journal) error {
		// the journal says what was renamed, not what was planned
		if op, ok := j.(interface{ SetSummary(string) }); ok {
			defer func() { op.SetSummary(RenameSummary(res.Folders, res.Files)) }()
		}
		return library.ApplyRenamesContext(ctx, afs.Afero, LibraryModes(), lib, j, renames, onProgress, func(i int, err error) {
			r := renames[i]
			switch {
			case errors.Is(err, library.ErrNotEmpty):
				res.Kept++
			case err != nil:
				res.Failed++
			case r.RemoveEmpty:
			case r.Chapter:
				res.Files++
			default:
				res.Folders++
			}
			if done != nil {
				done(i, err)
			}
		})
	})
	res.Snapshot = snapshot
	return res, err
}

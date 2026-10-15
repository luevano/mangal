package core

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/library"
)

// RenameReq renames manga (and provider) folders to the current name templates
// ("library rename"): Refresh gets the AniList metadata again, Chapters renames the
// chapter files too, Fallback the folders whose metadata isn't found. Paths keep only the
// renames of these folders and provider directories (a health finding's), Only (when
// not nil) only the renames from these paths (the selected ones). Cached only uses the
// cached metadata: folders without it aren't renamed.
type RenameReq struct {
	Refresh  bool     `json:"refresh,omitempty"`
	Chapters bool     `json:"chapters,omitempty"`
	Fallback bool     `json:"fallback,omitempty"`
	Cached   bool     `json:"cached,omitempty"`
	Paths    []string `json:"paths,omitempty"`
	Only     []string `json:"only,omitempty"`
}

func (RenameReq) Kind() Kind { return KindRename }

// forApply is the request planned again to apply it: from the metadata the plan cached.
func (r RenameReq) forApply() Change {
	r.Refresh = false
	return r
}

// planRenames is library rename's plan of the download directory (client.RenamePlan),
// only the renames of Paths when set.
func (r RenameReq) planRenames(ctx context.Context, c *Core) (client.RenamePlan, error) {
	files, err := c.existingFiles()
	if err != nil {
		return client.RenamePlan{}, err
	}
	dir := files.Root
	plan := client.RenamePlan{Root: dir}
	loaders, err := c.env.Loaders()
	if err != nil {
		return plan, err
	}
	namer := library.Namer{
		FS:           c.env.FS,
		Loaders:      loaders,
		ProviderName: c.env.ProviderName,
		MangaName:    c.env.MangaName,
		ChapterName:  c.env.ChapterName,
		Metadata:     c.env.MetadataBatch,
		Library:      files.Library,
		OnProgress:   progressOf(ctx),
		Fallback:     r.Fallback,
		OnDisagree: func(folder string, d []string) {
			for _, line := range d {
				plan.Disagree = append(plan.Disagree, fmt.Sprintf("%s: %s", plan.Rel(folder), line))
			}
		},
	}
	switch {
	case r.Cached:
		namer.Metadata, namer.NeedMetadata = c.env.MetadataCached, true
	case r.Refresh:
		namer.Metadata = c.env.MetadataFresh
	}
	providerDirs := c.env.ProviderDirs()
	renames, err := namer.Plan(ctx, dir, providerDirs)
	if err != nil {
		return plan, err
	}
	if r.Chapters {
		mangas, err := files.Library.Mangas()
		if err != nil {
			return plan, err
		}
		chapters, err := namer.PlanChapters(mangas)
		if err != nil {
			return plan, err
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
	if r.Paths != nil {
		renames = slices.DeleteFunc(renames, func(rn library.Rename) bool {
			return !slices.Contains(r.Paths, rn.From) && !slices.Contains(r.Paths, rn.ProviderDir)
		})
	}
	if r.Only != nil {
		renames = slices.DeleteFunc(renames, func(rn library.Rename) bool { return !slices.Contains(r.Only, rn.From) })
	}
	plan.Renames = renames
	if unmarked, err := library.Unmarked(c.env.FS, dir, providerDirs); err == nil {
		plan.Unmarked = len(unmarked)
	}
	return plan, nil
}

func (r RenameReq) plan(ctx context.Context, c *Core) (*prepared, error) {
	rp, err := r.planRenames(ctx, c)
	if err != nil {
		return nil, err
	}
	var lines []string
	var renames []library.Rename
	for _, rn := range rp.Renames {
		switch {
		case rn.NoMetadata && rn.Skip != "":
			// a warning
			continue
		}
		lines = append(lines, rp.Line(rn))
		renames = append(renames, rn)
	}
	var warnings []string
	if noMetadata := rp.NoMetadata(); len(noMetadata) != 0 {
		warnings = append(warnings, fmt.Sprintf("%d folders not renamed, no metadata found for their ids (the fallback name would drop the year and ids)", len(noMetadata)))
	}
	folders, chapterFiles := client.RenameCounts(renames)
	summary := client.RenameSummary(folders, chapterFiles)
	return &prepared{
		plan:   Plan{Summary: summary, Lines: lines, Warnings: warnings, Nothing: folders == 0 && chapterFiles == 0, Detail: rp},
		tables: libraryTables,
		apply: func(ctx context.Context, opts ApplyOpts, res *OpResult) error {
			var done client.RenameResult
			defer func() { res.Detail = done }()
			files := c.files()
			return c.journaled(ctx, "rename", summary, res, func(j library.Journal) error {
				// the journal says what was renamed, not what was planned
				if op, ok := j.(interface{ SetSummary(string) }); ok {
					defer func() { op.SetSummary(client.RenameSummary(done.Folders, done.Files)) }()
				}
				return library.ApplyRenamesContext(ctx, files.FS, files.Modes, files.Library, j, renames, progressOf(ctx), func(i int, err error) {
					rn := renames[i]
					switch {
					case errors.Is(err, library.ErrNotEmpty):
						done.Kept++
						opts.line(res, fmt.Sprintf("kept %s: not empty", rp.Rel(rn.From)), false)
					case err != nil:
						done.Failed++
						opts.line(res, fmt.Sprintf("skipped %s: %s", rp.Rel(rn.From), err), true)
					case rn.RemoveEmpty:
						opts.line(res, fmt.Sprintf("removed %s", rp.Rel(rn.From)), false)
					case rn.Chapter:
						done.Files++
						opts.line(res, rp.Line(rn), false)
					default:
						done.Folders++
						opts.line(res, rp.Line(rn), false)
					}
				})
			})
		},
	}, nil
}

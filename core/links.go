package core

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
)

// MoveReq moves the library manga to the provider's manga To (found by the caller): it
// gets it as a source, its folder goes to that provider's directory, its watch and
// bindings follow; Full adopts the new title and the folder name the template gives it.
type MoveReq struct {
	Manga    int64                  `json:"manga"`
	Provider libmangal.ProviderInfo `json:"-"`
	To       mangadata.Manga        `json:"-"`
	Full     bool                   `json:"full,omitempty"`
}

func (MoveReq) Kind() Kind { return KindMove }

func (c *Core) mover(ctx context.Context) (library.Mover, error) {
	files, err := c.existingFiles()
	if err != nil {
		return library.Mover{}, err
	}
	files.OnProgress = progressOf(ctx)
	return library.Mover{
		Files:        files,
		ProviderDirs: c.env.ProviderDirs(),
		ProviderName: c.env.ProviderName,
		MangaName:    c.env.MangaName,
		Metadata:     c.env.Metadata,
	}, nil
}

func (r MoveReq) plan(ctx context.Context, c *Core) (*prepared, error) {
	if r.To == nil || r.Provider.ID == "" {
		return nil, invalid("no manga to move to")
	}
	m, err := c.manga(r.Manga)
	if err != nil {
		return nil, err
	}
	mover, err := c.mover(ctx)
	if err != nil {
		return nil, err
	}
	mp, err := mover.PlanMove(ctx, m, r.Provider, r.To, library.MoveOptions{Full: r.Full})
	if err != nil {
		return nil, err
	}
	summary := fmt.Sprintf("move %d %q to %s %s", mp.Manga.ID, mp.Manga.Title, mp.Provider.ID, mp.To.ID)
	return &prepared{
		plan:   Plan{Summary: summary, Lines: mp.Lines(), Detail: mp},
		tables: append(append([]string{}, libraryTables...), watchTables...),
		apply: func(ctx context.Context, _ ApplyOpts, res *OpResult) error {
			return c.journaled(ctx, "move", summary, res, func(j library.Journal) error {
				mover.Journal, mover.OnProgress = j, progressOf(ctx)
				return mover.ApplyMoveContext(ctx, mp)
			})
		},
	}, nil
}

// LinkReq links a library manga to metadata sources (Links: source code → id, "" removes
// the link), checking the ids first. Rename and Watches also do what the links imply: the
// folder name the template gives, the bindings of its watches on other providers.
type LinkReq struct {
	Manga   int64         `json:"manga"`
	Links   library.Links `json:"links"`
	Rename  bool          `json:"rename,omitempty"`
	Watches bool          `json:"watches,omitempty"`
}

func (LinkReq) Kind() Kind { return KindLink }

// LinkChecked is an id checked before linking it: its metadata, nil when it couldn't be
// checked (MyAnimeList without an API client).
type LinkChecked struct {
	Code lmmeta.IDCode
	ID   string
	Meta lmmeta.Metadata
}

// LinkDetail is a link's plan: the ids checked and the library's plan.
type LinkDetail struct {
	Checked []LinkChecked
	Plan    library.LinkPlan
	// Renames and BindsWatches are the implied changes the request does.
	Renames, BindsWatches bool
}

func (c *Core) linker(ctx context.Context) (library.Linker, error) {
	files := c.files()
	files.OnProgress = progressOf(ctx)
	loaders, err := c.env.Loaders()
	if err != nil {
		return library.Linker{}, err
	}
	return library.Linker{Files: files, Loaders: loaders, MangaName: c.env.MangaName, Metadata: c.env.Metadata}, nil
}

func (r LinkReq) plan(ctx context.Context, c *Core) (*prepared, error) {
	m, err := c.manga(r.Manga)
	if err != nil {
		return nil, err
	}
	var d LinkDetail
	for _, src := range client.MetadataSources {
		id, ok := r.Links[src.Code]
		if !ok || id == "" {
			continue
		}
		report(ctx, library.Progress{Phase: fmt.Sprintf("checking %s %s", src.Name, id)})
		meta, err := c.env.CheckLink(ctx, src.Code, id)
		if err != nil {
			return nil, err
		}
		d.Checked = append(d.Checked, LinkChecked{Code: src.Code, ID: id, Meta: meta})
	}
	linker, err := c.linker(ctx)
	if err != nil {
		return nil, err
	}
	report(ctx, library.Progress{Phase: "planning"})
	lp, err := linker.PlanLinks(ctx, m, r.Links, library.PlanOptions{})
	if err != nil {
		return nil, err
	}
	d.Plan = lp
	tables := libraryTables
	if r.Watches || len(lp.Watches) != 0 {
		tables = append(append([]string{}, libraryTables...), watchTables...)
	}
	// the variants are the same links with or without what they imply
	var build func(r LinkReq) *prepared
	build = func(r LinkReq) *prepared {
		d := d
		opts := library.LinkOptions{Rename: r.Rename, Watches: r.Watches}
		d.Renames = opts.Rename && lp.Rename != nil && lp.Rename.Skip == ""
		d.BindsWatches = opts.Watches && len(lp.Watches) != 0
		lines := lp.Lines()
		if d.Renames {
			lines = append(lines, "rename to "+filepath.Base(lp.Rename.To))
		}
		if d.BindsWatches {
			lines = append(lines, fmt.Sprintf("bind %d watches", len(lp.Watches)))
		}
		summary := fmt.Sprintf("link %d %q", m.ID, m.Title)
		return &prepared{
			plan:   Plan{Summary: summary, Lines: lines, Nothing: len(lp.Changes) == 0 && !d.Renames && !d.BindsWatches, Detail: d},
			tables: tables,
			apply: func(ctx context.Context, _ ApplyOpts, res *OpResult) error {
				return c.journaled(ctx, "link", summary, res, func(j library.Journal) error {
					linker.Journal, linker.OnProgress = j, progressOf(ctx)
					return linker.ApplyLinksContext(ctx, lp, opts)
				})
			},
			vary: func(ch Change) (*prepared, bool) {
				v, ok := ch.(LinkReq)
				if !ok || v.Manga != r.Manga || !maps.Equal(v.Links, r.Links) {
					return nil, false
				}
				return build(v), true
			},
		}
	}
	return build(r), nil
}

// BackupsReq fills the Kitsu and MyAnimeList links of the library mangas (nil: all)
// linked to AniList; a link that differs is only replaced for the mangas of Replace (or
// every one with ReplaceAll).
type BackupsReq struct {
	Mangas     []int64 `json:"mangas,omitempty"`
	Replace    []int64 `json:"replace,omitempty"`
	ReplaceAll bool    `json:"replace_all,omitempty"`
}

func (BackupsReq) Kind() Kind { return KindBackups }

// BackupsDetail is what the backups found.
type BackupsDetail struct {
	Backups []library.Backup
	// Summary says what was found (BackupFinder.Summary).
	Summary string
	// Fill and Conflicts count the links to fill and those that differ, ToSet the mangas
	// whose links would be set.
	Fill, Conflicts, ToSet int
}

func (r BackupsReq) replace(id int64) bool {
	if r.ReplaceAll {
		return true
	}
	for _, m := range r.Replace {
		if m == id {
			return true
		}
	}
	return false
}

func (r BackupsReq) plan(ctx context.Context, c *Core) (*prepared, error) {
	lib := c.env.Storage.Library()
	var mangas []storage.Manga
	if r.Mangas == nil {
		var err error
		if mangas, err = lib.Mangas(); err != nil {
			return nil, err
		}
	}
	for _, id := range r.Mangas {
		m, err := c.manga(id)
		if err != nil {
			return nil, err
		}
		mangas = append(mangas, m)
	}
	finder, err := c.env.BackupFinder()
	if err != nil {
		return nil, err
	}
	finder.Files = c.files()
	finder.OnProgress = progressOf(ctx)
	backups, err := finder.Find(ctx, mangas)
	if err != nil {
		return nil, err
	}
	linker, err := c.linker(ctx)
	if err != nil {
		return nil, err
	}
	return r.prepare(c, backups, finder.Summary(backups), linker), nil
}

// prepare is the plan of the backups found, its variants replace other links.
func (r BackupsReq) prepare(c *Core, backups []library.Backup, found string, linker library.Linker) *prepared {
	d := BackupsDetail{Backups: backups, Summary: found}
	d.Fill, d.Conflicts, _, _ = library.BackupSummary(backups)
	var lines []string
	for _, b := range backups {
		for _, line := range b.Lines() {
			lines = append(lines, "  "+line)
		}
		if len(b.Links(r.replace(b.Manga.ID))) != 0 {
			d.ToSet++
		}
	}
	summary := fmt.Sprintf("backup links of %d mangas", d.ToSet)
	return &prepared{
		plan:   Plan{Summary: summary, Lines: append(lines, d.Summary), Nothing: d.ToSet == 0, Detail: d},
		tables: libraryTables,
		vary: func(ch Change) (*prepared, bool) {
			v, ok := ch.(BackupsReq)
			if !ok || !slices.Equal(v.Mangas, r.Mangas) {
				return nil, false
			}
			return v.prepare(c, backups, found, linker), true
		},
		apply: func(ctx context.Context, _ ApplyOpts, res *OpResult) error {
			n := 0
			// Detail is how many mangas got their links
			defer func() { res.Detail = n }()
			return c.journaled(ctx, "link", summary, res, func(j library.Journal) error {
				linker.Journal = j
				for i, b := range backups {
					links := b.Links(r.replace(b.Manga.ID))
					if len(links) == 0 {
						continue
					}
					report(ctx, library.Progress{Phase: "linking", Done: i + 1, Total: len(backups), Item: b.Manga.Title})
					plan, err := linker.PlanLinks(ctx, b.Manga, links, library.PlanOptions{NoImplied: true})
					if err != nil {
						return err
					}
					if err := linker.ApplyLinksContext(ctx, plan, library.LinkOptions{}); err != nil {
						return err
					}
					n++
				}
				return nil
			})
		},
	}
}

// KindDismissPick is dismissing a link pick.
const KindDismissPick Kind = "dismiss-pick"

// DismissPickReq dismisses the library manga's pick of the source (the ids its sources
// disagree on): nothing is linked and it isn't asked again until other ids are found.
// It isn't journaled, nothing is lost: linking one of the ids later settles it.
type DismissPickReq struct {
	Manga  int64         `json:"manga"`
	Source lmmeta.IDCode `json:"source"`
}

func (DismissPickReq) Kind() Kind { return KindDismissPick }

func (r DismissPickReq) plan(_ context.Context, c *Core) (*prepared, error) {
	m, err := c.manga(r.Manga)
	if err != nil {
		return nil, err
	}
	src, ok := client.MetadataSourceOf(r.Source)
	if !ok {
		return nil, invalid("no metadata source %q", r.Source)
	}
	lib := c.env.Storage.Library()
	pick, found, err := lib.LinkPick(m.ID, r.Source)
	if err != nil {
		return nil, err
	}
	if !found || pick.Dismissed {
		msg := fmt.Sprintf("library manga %d has no %s ids to pick from", m.ID, src.Name)
		return &prepared{plan: Plan{Lines: []string{msg}, Nothing: true}, tables: libraryTables}, nil
	}
	summary := fmt.Sprintf("dismiss the %s pick of %d %q", src.Name, m.ID, m.Title)
	line := fmt.Sprintf("%s (%s): nothing linked, not asked again until other ids are found", summary, strings.Join(pick.IDs(), " or "))
	return &prepared{
		plan:   Plan{Summary: summary, Lines: []string{line}, Detail: pick},
		tables: libraryTables,
		apply: func(context.Context, ApplyOpts, *OpResult) error {
			_, err := lib.DismissLinkPick(m.ID, r.Source)
			return err
		},
	}, nil
}

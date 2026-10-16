package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/health"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// KindHealthFix is a health finding's fix.
const KindHealthFix Kind = "health-fix"

// HealthFixReq applies the fix of a health finding (its Fix.Action) as the change it maps
// to: a link or pick is a LinkReq (Choice: the id, when there are several), mark a
// MarkReq, forget a ForgetReq, rename a RenameReq of its paths (cached metadata), merge a
// MergeReq (Choice: the library manga to keep, the suggested one by default), empty-trash
// an EmptyTrashReq, backups links (journaled as a LinkReq) the backup ids still missing
// (a summary's: each item's); resume and watch change the watches. A scan isn't a change
// (*health.NotFixableError). Plan.Detail and OpResult.Detail are a health.FixResult.
type HealthFixReq struct {
	Finding health.Finding `json:"finding"`
	Choice  string         `json:"choice,omitempty"`
}

func (HealthFixReq) Kind() Kind { return KindHealthFix }

// gone is a fix with nothing to do any more.
func gone(f health.Finding, msg string) *prepared {
	res := health.FixResult{Key: f.Key, Kind: f.Fix.Action.Kind, Plan: []string{}, Nothing: true, Message: msg}
	return &prepared{plan: Plan{Lines: []string{msg}, Nothing: true, Detail: res}, tables: libraryTables}
}

func (r HealthFixReq) plan(ctx context.Context, c *Core) (*prepared, error) {
	f := r.Finding
	a := f.Fix.Action
	if a == nil {
		return nil, &health.NotFixableError{Commands: f.Fix.Commands}
	}
	exists := func(id int64) (storage.Manga, bool, error) {
		m, err := c.manga(id)
		if errors.Is(err, ErrNotFound) {
			return m, false, nil
		}
		return m, err == nil, err
	}
	var inner Change
	// done is the message of the fix applied, nothing the plan's when there's nothing to do
	var done func(res OpResult) string
	nothing := "nothing to do any more"
	switch a.Kind {
	case health.ActionLink, health.ActionPick:
		id := r.Choice
		switch {
		case id == "" && len(a.IDs) == 1:
			id = a.IDs[0]
		case id == "":
			return nil, fmt.Errorf("%w: choose one of %s", health.ErrChoice, strings.Join(a.IDs, ", "))
		case !slices.Contains(a.IDs, id):
			return nil, fmt.Errorf("%w: %s isn't one of %s", health.ErrChoice, id, strings.Join(a.IDs, ", "))
		}
		m, ok, err := exists(a.Manga)
		if err != nil {
			return nil, err
		}
		if !ok {
			return gone(f, fmt.Sprintf("library manga %d is gone", a.Manga)), nil
		}
		inner, nothing = LinkReq{Manga: m.ID, Links: library.Links{a.Source: id}}, "nothing to change"
		done = func(OpResult) string {
			return fmt.Sprintf("linked %s to %s %s", m.Title, client.SourceName(a.Source), id)
		}
	case health.ActionResume, health.ActionWatch:
		return r.planWatch(ctx, c)
	case health.ActionBackups:
		return r.planBackups(ctx, c)
	case health.ActionMark:
		ids := a.Mangas
		if a.Manga != 0 {
			ids = []int64{a.Manga}
		}
		var have []int64
		var title string
		for _, id := range ids {
			m, ok, err := exists(id)
			if err != nil {
				return nil, err
			}
			if ok {
				have, title = append(have, id), m.Title
			}
		}
		if len(have) == 0 {
			return gone(f, "nothing to mark any more"), nil
		}
		inner, nothing = MarkReq{Mangas: have}, "nothing to mark any more"
		done = func(res OpResult) string {
			marked, _ := res.Detail.(library.MarkResult)
			if len(have) == 1 && len(marked.Marked) == 1 {
				return "marked " + title
			}
			return "marked " + stringutil.Quantify(len(marked.Marked), "folder", "folders")
		}
	case health.ActionForget:
		m, ok, err := exists(a.Manga)
		if err != nil {
			return nil, err
		}
		if !ok {
			return gone(f, fmt.Sprintf("library manga %d is gone", a.Manga)), nil
		}
		inner = ForgetReq{Mangas: []int64{m.ID}}
		done = func(OpResult) string { return "forgot " + m.Title }
	case health.ActionRename:
		inner, nothing = RenameReq{Paths: a.Paths, Cached: true}, "nothing to rename any more"
		done = func(res OpResult) string {
			renamed, _ := res.Detail.(client.RenameResult)
			msg := "renamed " + stringutil.Quantify(renamed.Folders, "folder", "folders")
			if renamed.Failed != 0 {
				msg += fmt.Sprintf(", %d skipped (mangal library rename says why)", renamed.Failed)
			}
			return msg
		}
	case health.ActionMerge:
		var mangas []storage.Manga
		for _, id := range a.Mangas {
			m, ok, err := exists(id)
			if err != nil {
				return nil, err
			}
			if ok {
				mangas = append(mangas, m)
			}
		}
		if len(mangas) < 2 {
			return gone(f, "nothing to merge any more"), nil
		}
		keep := mangas[0]
		if r.Choice != "" {
			i := slices.IndexFunc(mangas, func(m storage.Manga) bool { return strconv.FormatInt(m.ID, 10) == r.Choice })
			if i < 0 {
				return nil, fmt.Errorf("%w: keep one of %s", health.ErrChoice, mangaIDs(mangas))
			}
			keep = mangas[i]
		}
		other := mangas[0]
		if other.ID == keep.ID {
			other = mangas[1]
		}
		inner = MergeReq{Keep: keep.ID, Other: other.ID}
		done = func(OpResult) string { return fmt.Sprintf("merged %s into %s", other.Title, keep.Title) }
	case health.ActionEmptyTrash:
		inner, nothing = EmptyTrashReq{}, "the trash is empty"
		done = func(OpResult) string { return "trash emptied" }
	default:
		return nil, &health.NotFixableError{Kind: a.Kind, Commands: f.Fix.Commands}
	}
	p, err := inner.plan(ctx, c)
	if err != nil {
		return nil, err
	}
	fix := health.FixResult{Key: f.Key, Kind: a.Kind, Plan: p.plan.Lines, Nothing: p.plan.Nothing}
	if fix.Plan == nil {
		fix.Plan = []string{}
	}
	if fix.Nothing {
		fix.Message = nothing
	}
	wrapped := *p
	wrapped.plan.Detail = fix
	if p.apply != nil {
		wrapped.apply = func(ctx context.Context, opts ApplyOpts, res *OpResult) error {
			if err := p.apply(ctx, opts, res); err != nil {
				return err
			}
			fix.Applied, fix.Message, fix.Snapshot = true, done(*res), res.Snapshot
			res.Detail = fix
			return nil
		}
	}
	return &wrapped, nil
}

// planBackups gives the library manga, or each of the summary's items, the backup links
// it still has no id of: one journaled link.
func (r HealthFixReq) planBackups(ctx context.Context, c *Core) (*prepared, error) {
	f := r.Finding
	todo := []health.Action{*f.Fix.Action}
	if len(f.Items) != 0 {
		todo = nil
		for _, item := range f.Items {
			if a := item.Fix.Action; a != nil && a.Kind == health.ActionBackups && a.Manga != 0 {
				todo = append(todo, *a)
			}
		}
	}
	linker, err := c.linker(ctx)
	if err != nil {
		return nil, err
	}
	var plans []library.LinkPlan
	var lines []string
	for _, a := range todo {
		m, err := c.manga(a.Manga)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		info, err := linker.LinkInfo(m)
		if err != nil {
			return nil, err
		}
		links := library.Links{}
		for code, id := range a.Links {
			// one linked meanwhile keeps its id
			if id != "" && !info.Source(code).Linked() {
				links[code] = id
			}
		}
		if len(links) == 0 {
			continue
		}
		lp, err := linker.PlanLinks(ctx, m, links, library.PlanOptions{NoImplied: true})
		if err != nil {
			return nil, err
		}
		if len(lp.Changes) != 0 {
			plans = append(plans, lp)
			lines = append(lines, lp.Lines()...)
		}
	}
	fix := health.FixResult{Key: f.Key, Kind: health.ActionBackups, Plan: lines}
	if len(plans) == 0 {
		return gone(f, "nothing to set any more"), nil
	}
	summary := "backup links of " + stringutil.Quantify(len(plans), "manga", "mangas")
	if len(plans) == 1 {
		summary = fmt.Sprintf("backup links of %d %q", plans[0].Manga().ID, plans[0].Manga().Title)
	}
	return &prepared{
		plan:   Plan{Summary: summary, Lines: lines, Detail: fix},
		tables: libraryTables,
		apply: func(ctx context.Context, _ ApplyOpts, res *OpResult) error {
			err := c.journaled(ctx, "link", summary, res, func(j library.Journal) error {
				linker.Journal = j
				for i, lp := range plans {
					report(ctx, library.Progress{Phase: "linking", Done: i + 1, Total: len(plans), Item: lp.Manga().Title})
					if err := linker.ApplyLinksContext(ctx, lp, library.LinkOptions{}); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
			fix.Applied, fix.Message, fix.Snapshot = true, "set the "+summary, res.Snapshot
			res.Detail = fix
			return nil
		},
	}, nil
}

func mangaIDs(mangas []storage.Manga) string {
	ids := make([]string, len(mangas))
	for i, m := range mangas {
		ids[i] = strconv.FormatInt(m.ID, 10)
	}
	return strings.Join(ids, ", ")
}

// planWatch is a resume or a watch: changes of the watches, not the library.
func (r HealthFixReq) planWatch(ctx context.Context, c *Core) (*prepared, error) {
	f := r.Finding
	a := f.Fix.Action
	m, err := c.env.Watches(ctx)
	if err != nil {
		return nil, err
	}
	fix := health.FixResult{Key: f.Key, Kind: a.Kind}
	var apply func(ctx context.Context) (string, error)
	switch a.Kind {
	case health.ActionResume:
		w, err := m.Get(ctx, a.Watch)
		if err != nil {
			return nil, err
		}
		if w.Enabled {
			fix.Plan, fix.Nothing, fix.Message = []string{}, true, fmt.Sprintf("watch %d is enabled", w.ID)
			return &prepared{plan: Plan{Lines: []string{fix.Message}, Nothing: true, Detail: fix}, tables: watchTables, nolock: true}, nil
		}
		fix.Plan = []string{fmt.Sprintf("resume watch %d %s (%s)", w.ID, w.Title, w.Provider)}
		apply = func(ctx context.Context) (string, error) {
			if _, err := m.SetEnabled(ctx, w.ID, true); err != nil {
				return "", err
			}
			return "resumed " + w.Title, nil
		}
	default:
		fix.Plan = []string{fmt.Sprintf("watch %s on %s (mid:%s): its first check downloads the chapters after the library's last", f.Subject.Name, a.Provider, a.MangaID)}
		apply = func(ctx context.Context) (string, error) {
			w, err := m.Add(ctx, watch.AddOptions{Provider: a.Provider, MangaID: a.MangaID})
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("watching %s on %s (watch %d)", w.Title, w.Provider, w.ID), nil
		}
	}
	return &prepared{
		plan:   Plan{Summary: fix.Plan[0], Lines: fix.Plan, Detail: fix},
		tables: watchTables,
		nolock: true,
		apply: func(ctx context.Context, _ ApplyOpts, res *OpResult) error {
			msg, err := apply(ctx)
			if err != nil {
				return err
			}
			fix.Applied, fix.Message = true, msg
			res.Detail = fix
			return nil
		},
	}, nil
}

// Finding is the finding with the key there now, an offline run of its check (with the
// network checks too when network), a *NotFoundError when it isn't there.
func (c *Core) Finding(ctx context.Context, key string, network bool) (health.Finding, error) {
	check, subject, err := health.SplitKey(key)
	if err != nil {
		return health.Finding{}, invalid("%s", err)
	}
	m, err := c.env.Watches(ctx)
	if err != nil {
		return health.Finding{}, err
	}
	checker, err := health.NewWith(m)
	if err != nil {
		return health.Finding{}, err
	}
	report, err := checker.Run(ctx, health.Options{Only: []string{check}, Network: network, All: true})
	if err != nil {
		return health.Finding{}, err
	}
	f, ok := report.Find(check + "/" + subject)
	if !ok {
		what := fmt.Sprintf("no finding %s now (mangal health shows the keys", key)
		if health.IsNetwork(check) && !network {
			what += "; a network check's needs --network"
		}
		return health.Finding{}, &NotFoundError{What: what + ")"}
	}
	return f, nil
}

package core

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/health"
	"github.com/luevano/mangal/v5/storage"
)

type fixManager struct {
	watch.Manager
	enabled map[int64]bool
	added   []watch.AddOptions
}

func (m *fixManager) Get(_ context.Context, id int64) (watch.Watch, error) {
	return watch.Watch{Watch: storage.Watch{ID: id, Title: "Vagabond", Provider: "mango-mangadex", Enabled: m.enabled[id]}}, nil
}

func (m *fixManager) SetEnabled(_ context.Context, id int64, enabled bool) (watch.Watch, error) {
	m.enabled[id] = enabled
	return m.Get(context.Background(), id)
}

func (m *fixManager) Add(_ context.Context, opts watch.AddOptions) (watch.Watch, error) {
	m.added = append(m.added, opts)
	return watch.Watch{Watch: storage.Watch{ID: 10, Title: "Kagurabachi", Provider: opts.Provider}}, nil
}

// fix plans the finding's fix, applies it with apply.
func fix(t *testing.T, c *Core, f health.Finding, choice string, apply bool) (health.FixResult, error) {
	t.Helper()
	ctx := context.Background()
	p, err := c.Changes.Plan(ctx, HealthFixReq{Finding: f, Choice: choice})
	if err != nil {
		return health.FixResult{}, err
	}
	if !apply || p.Nothing {
		return p.Detail.(health.FixResult), nil
	}
	res, err := c.Changes.ApplyPlan(ctx, p, ApplyOpts{})
	if err != nil {
		return health.FixResult{}, err
	}
	return res.Detail.(health.FixResult), nil
}

func TestHealthFixes(t *testing.T) {
	c, root := testLinkCore(t)
	m := &fixManager{enabled: map[int64]bool{}}
	env := c.env
	env.Watches = func(context.Context) (watch.Manager, error) { return m, nil }
	c, _ = New(context.Background(), env)
	berserk := addManga(t, c, "Berserk", "p", "b", "Berserk", 1)
	other := addManga(t, c, "Berserk 2", "q", "b2", "Berserk", 2)

	// a link: the choice is one of the ids
	link := health.Finding{Key: fmt.Sprintf("M1/manga/%d", berserk.ID), Fix: health.Fix{Action: &health.Action{Kind: health.ActionLink, Manga: berserk.ID, Source: metadata.IDCodeAnilist, IDs: []string{"30002", "1"}}}}
	if _, err := fix(t, c, link, "", false); !errors.Is(err, health.ErrChoice) {
		t.Errorf("no choice: %v", err)
	}
	if _, err := fix(t, c, link, "7", false); !errors.Is(err, health.ErrChoice) {
		t.Errorf("a choice not offered: %v", err)
	}
	plan, err := fix(t, c, link, "30002", false)
	if err != nil || plan.Applied || plan.Nothing || len(plan.Plan) == 0 || plan.Kind != health.ActionLink {
		t.Fatalf("plan %+v %v", plan, err)
	}
	res, err := fix(t, c, link, "30002", true)
	if err != nil || !res.Applied || res.Snapshot == "" || res.Message != "linked Berserk to AniList 30002" {
		t.Fatalf("applied %+v %v", res, err)
	}
	if again, err := fix(t, c, link, "30002", true); err != nil || !again.Nothing || again.Applied {
		t.Errorf("again %+v %v", again, err)
	}

	// the watches
	resume := health.Finding{Key: "W3/watch/4", Fix: health.Fix{Action: &health.Action{Kind: health.ActionResume, Watch: 4}}}
	if res, err := fix(t, c, resume, "", false); err != nil || res.Applied || m.enabled[4] || !strings.Contains(res.Plan[0], "resume watch 4") {
		t.Fatalf("resume plan %+v %v", res, err)
	}
	if res, err := fix(t, c, resume, "", true); err != nil || !res.Applied || !m.enabled[4] || res.Message != "resumed Vagabond" {
		t.Fatalf("resume %+v %v", res, err)
	}
	add := health.Finding{Key: "W12/manga/8", Subject: health.Subject{Kind: "manga", ID: 8, Name: "Kagurabachi"},
		Fix: health.Fix{Action: &health.Action{Kind: health.ActionWatch, Manga: 8, Provider: "mango-mangadex", MangaID: "kagura"}}}
	if res, err := fix(t, c, add, "", true); err != nil || !res.Applied || len(m.added) != 1 || m.added[0].MangaID != "kagura" {
		t.Fatalf("watch %+v %v %+v", res, err, m.added)
	}

	// mark, a gone manga left out
	mark := health.Finding{Fix: health.Fix{Action: &health.Action{Kind: health.ActionMark, Mangas: []int64{berserk.ID, 99}}}}
	if res, err := fix(t, c, mark, "", true); err != nil || !res.Applied || res.Message != "marked Berserk" {
		t.Fatalf("mark %+v %v", res, err)
	}
	if res, _ := fix(t, c, mark, "", false); !res.Nothing || res.Message != "nothing to mark any more" {
		t.Errorf("marked again %+v", res)
	}

	// rename, from the cached metadata
	rename := health.Finding{Fix: health.Fix{Action: &health.Action{Kind: health.ActionRename, Paths: []string{filepath.Join(root, "Berserk")}}}}
	res, err = fix(t, c, rename, "", false)
	if err != nil || !slices.Equal(res.Plan, []string{"Berserk -> Berserk (1989) [alid-30002]"}) {
		t.Fatalf("rename plan %+v %v", res, err)
	}
	if res, err := fix(t, c, rename, "", true); err != nil || res.Message != "renamed 1 folder" {
		t.Fatalf("rename %+v %v", res, err)
	}

	// merge: the suggested keep, or the choice
	merge := health.Finding{Fix: health.Fix{Action: &health.Action{Kind: health.ActionMerge, Mangas: []int64{berserk.ID, other.ID}}}}
	if _, err := fix(t, c, merge, "5", false); !errors.Is(err, health.ErrChoice) {
		t.Errorf("keep not offered: %v", err)
	}
	if res, err := fix(t, c, merge, fmt.Sprint(other.ID), false); err != nil || !strings.Contains(res.Plan[0], `keep "Berserk" (`+filepath.Join(root, "Berserk 2")) {
		t.Fatalf("merge plan %+v %v", res, err)
	}
	if res, err := fix(t, c, merge, "", true); err != nil || res.Message != "merged Berserk into Berserk" {
		t.Fatalf("merge %+v %v", res, err)
	}

	// forget a manga gone: nothing
	forget := health.Finding{Fix: health.Fix{Action: &health.Action{Kind: health.ActionForget, Manga: other.ID}}}
	if res, err := fix(t, c, forget, "", true); err != nil || !res.Nothing {
		t.Errorf("forget a merged manga %+v %v", res, err)
	}
	forget.Fix.Action.Manga = berserk.ID
	if res, err := fix(t, c, forget, "", true); err != nil || res.Message != "forgot Berserk" {
		t.Errorf("forget %+v %v", res, err)
	}

	// the trash: the merge's folder was empty
	trash := health.Finding{Fix: health.Fix{Action: &health.Action{Kind: health.ActionEmptyTrash}}}
	if res, err := fix(t, c, trash, "", true); err != nil || !res.Nothing || res.Message != "the trash is empty" {
		t.Errorf("empty the trash %+v %v", res, err)
	}

	// a scan isn't a change
	var nf *health.NotFixableError
	scan := health.Finding{Fix: health.Fix{Commands: []string{"mangal library scan"}, Action: &health.Action{Kind: health.ActionScan}}}
	if _, err := fix(t, c, scan, "", false); !errors.As(err, &nf) || !strings.Contains(err.Error(), "mangal library scan") {
		t.Errorf("scan: %v", err)
	}
	if _, err := fix(t, c, health.Finding{}, "", false); !errors.As(err, &nf) {
		t.Errorf("no action: %v", err)
	}
}

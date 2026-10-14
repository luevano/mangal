package health

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/library"
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

func newFixer(t *testing.T) (Fixer, *fixManager, *[]string) {
	c := newChecker(t)
	m := &fixManager{enabled: map[int64]bool{}}
	var ops []string
	return Fixer{
		Watches: m,
		Linker:  library.Linker{Files: library.Files{FS: c.FS, Library: c.Library, Root: "/dl"}, Loaders: c.Loaders},
		Operation: func(kind, summary string, do func(library.Journal) error) (string, error) {
			ops = append(ops, kind+": "+summary)
			return "snap-1", do(nil)
		},
	}, m, &ops
}

func TestFixLink(t *testing.T) {
	x, _, ops := newFixer(t)
	f := Finding{Key: "M1/manga/4", Fix: Fix{Action: &Action{Kind: ActionLink, Manga: 4, Source: metadata.IDCodeAnilist, IDs: []string{"656"}}}}
	plan, err := x.Fix(context.Background(), f, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Applied || plan.Nothing || len(plan.Plan) == 0 || len(*ops) != 0 {
		t.Fatalf("plan %+v, ops %q", plan, *ops)
	}
	if _, err := x.Fix(context.Background(), f, "7", false); !errors.Is(err, ErrChoice) {
		t.Errorf("a choice not offered: %v", err)
	}
	res, err := x.Fix(context.Background(), f, "656", true)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Applied || res.Snapshot != "snap-1" || !slices.Equal(*ops, []string{`link: link 4 "Vagabond"`}) {
		t.Errorf("applied %+v, ops %q", res, *ops)
	}
	m, _, _ := x.Linker.Library.MangaByID(4)
	if !slices.ContainsFunc(m.MetadataIDs, func(id metadata.ID) bool { return id.Raw == "656" }) {
		t.Errorf("not linked: %+v", m.MetadataIDs)
	}
	// done: nothing any more
	if again, err := x.Fix(context.Background(), f, "", true); err != nil || !again.Nothing || again.Applied {
		t.Errorf("again %+v %v", again, err)
	}
}

func TestFixWatches(t *testing.T) {
	x, m, _ := newFixer(t)
	resume := Finding{Key: "W3/watch/4", Fix: Fix{Action: &Action{Kind: ActionResume, Watch: 4}}}
	if res, err := x.Fix(context.Background(), resume, "", false); err != nil || res.Applied || m.enabled[4] || !strings.Contains(res.Plan[0], "resume watch 4") {
		t.Fatalf("plan %+v %v", res, err)
	}
	if res, err := x.Fix(context.Background(), resume, "", true); err != nil || !res.Applied || !m.enabled[4] {
		t.Fatalf("apply %+v %v", res, err)
	}
	add := Finding{Key: "W12/manga/8", Subject: Subject{Kind: "manga", ID: 8, Name: "Kagurabachi"},
		Fix: Fix{Action: &Action{Kind: ActionWatch, Manga: 8, Provider: "mango-mangadex", MangaID: "kagura"}}}
	if res, err := x.Fix(context.Background(), add, "", true); err != nil || !res.Applied || len(m.added) != 1 || m.added[0].MangaID != "kagura" {
		t.Fatalf("watch %+v %v %+v", res, err, m.added)
	}
	var nf *NotFixableError
	merge := Finding{Fix: Fix{Commands: []string{"mangal library merge 1 2"}, Action: &Action{Kind: ActionMerge, Mangas: []int64{1, 2}}}}
	if _, err := x.Fix(context.Background(), merge, "", false); !errors.As(err, &nf) || !strings.Contains(err.Error(), "mangal library merge 1 2") {
		t.Errorf("merge: %v", err)
	}
}

func TestFixForgetAndMark(t *testing.T) {
	x, _, ops := newFixer(t)
	mark := Finding{Fix: Fix{Action: &Action{Kind: ActionMark, Manga: 5}}}
	res, err := x.Fix(context.Background(), mark, "", true)
	if err != nil || !res.Applied || len(res.Plan) != 1 || !strings.HasSuffix(res.Plan[0], "Dungeon Odyssey [alid-555]/.libmangal.json") {
		t.Fatalf("mark %+v %v", res, err)
	}
	forget := Finding{Fix: Fix{Action: &Action{Kind: ActionForget, Manga: 4}}}
	if res, err = x.Fix(context.Background(), forget, "", true); err != nil || !res.Applied {
		t.Fatalf("forget %+v %v", res, err)
	}
	if _, ok, _ := x.Linker.Library.MangaByID(4); ok {
		t.Error("not forgotten")
	}
	if want := []string{"mark: mark 1 folders", `remove: remove missing 4 "Vagabond"`}; !slices.Equal(*ops, want) {
		t.Errorf("ops %q", *ops)
	}
}

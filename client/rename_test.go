package client

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/util/cache"
)

func setDownloads(t *testing.T, dir string) {
	t.Helper()
	old := config.Download.Path.Get()
	if err := config.Download.Path.Set(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.Download.Path.Set(old) })
}

func TestPlanLibraryRenameNoDownloads(t *testing.T) {
	lib, err := cache.Library()
	if err != nil {
		t.Fatal(err)
	}
	setDownloads(t, filepath.Join(t.TempDir(), "gone"))
	if _, err := PlanLibraryRename(context.Background(), lib, RenameOptions{}); !errors.Is(err, path.ErrNoDownloads) {
		t.Errorf("missing download path: %v", err)
	}
}

func TestRenameLines(t *testing.T) {
	p := RenamePlan{Root: "/dl"}
	for _, c := range []struct {
		r    library.Rename
		want string
	}{
		{library.Rename{From: "/dl/A", To: "/dl/B"}, "A -> B"},
		{library.Rename{From: "/dl/A/1.cbz", To: "/dl/A/[1] One.cbz", Chapter: true}, "A/1.cbz -> [1] One.cbz"},
		{library.Rename{From: "/dl/P", RemoveEmpty: true}, "remove P once empty"},
		{library.Rename{From: "/dl/A", To: "/dl/B", Skip: "taken"}, "skip A: taken"},
	} {
		if got := p.Line(c.r); got != c.want {
			t.Errorf("%+v: %q, want %q", c.r, got, c.want)
		}
	}
	if got := RenameSummary(RenameCounts([]library.Rename{{From: "a"}, {From: "b", Chapter: true}, {From: "c", Skip: "x"}})); got != "rename 1 folders, 1 chapter files" {
		t.Errorf("summary %q", got)
	}
}

// the rename is one operation, its summary what was renamed, undone by library undo
func TestApplyLibraryRename(t *testing.T) {
	lib, err := cache.Library()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	setDownloads(t, root)
	from, to := filepath.Join(root, "Berserk"), filepath.Join(root, "Berserk (1989)")
	_ = os.MkdirAll(from, 0o755)
	renames := []library.Rename{{From: from, To: to}, {From: root, Skip: "taken"}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ApplyLibraryRename(ctx, lib, renames, nil, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
	var told []int
	res, err := ApplyLibraryRename(context.Background(), lib, renames, nil, func(i int, _ error) { told = append(told, i) })
	if err != nil {
		t.Fatal(err)
	}
	if res.Folders != 1 || res.Failed != 1 || len(told) != 2 || res.Snapshot == "" {
		t.Errorf("result %+v, told %v", res, told)
	}
	if _, err := os.Stat(to); err != nil {
		t.Fatal("not renamed")
	}
	undoer, err := LibraryUndoer()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := undoer.Plan(0)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Op.Kind != "rename" || plan.Op.Summary != "rename 1 folders" {
		t.Fatalf("last op %+v", plan.Op)
	}
	if _, _, err := Undo(undoer, plan, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(from); err != nil {
		t.Error("not renamed back")
	}
}

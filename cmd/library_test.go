package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/spf13/cobra"
)

// runCmd runs the command's RunE with args, its output is out.
func runCmd(t *testing.T, c *cobra.Command, run func() error) (out string, err error) {
	t.Helper()
	var b bytes.Buffer
	c.SetContext(context.Background())
	c.SetOut(&b)
	c.SetErr(&b)
	err = run()
	return b.String(), err
}

func touchFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(path), 0o644); err != nil {
		t.Fatal(err)
	}
}

// scan, merge --apply, trash empty --yes and the undo they leave, on a download path of
// two folders of the same manga.
func TestLibraryScanMergeTrash(t *testing.T) {
	root := filepath.Join(t.TempDir(), "dl")
	touchFile(t, filepath.Join(root, "Keep", "Chapter 1.cbz"))
	touchFile(t, filepath.Join(root, "Other", "Chapter 1.cbz"))
	touchFile(t, filepath.Join(root, "Other", "Chapter 2.cbz"))
	old := config.Download.Path.Get()
	if err := config.Download.Path.Set(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.Download.Path.Set(old) })

	out, err := runCmd(t, libraryScanCmd, func() error { return libraryScanCmd.RunE(libraryScanCmd, nil) })
	if err != nil || !strings.Contains(out, "added 3") {
		t.Fatalf("scan: %v\n%s", err, out)
	}
	lib, err := cache.Library()
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]int64{}
	mangas, err := lib.Mangas()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range mangas {
		if strings.HasPrefix(m.Path(), root) {
			ids[m.Dir] = m.ID
		}
	}
	if len(ids) != 2 {
		t.Fatalf("mangas %+v", mangas)
	}
	args := []string{strconv.FormatInt(ids["Keep"], 10), strconv.FormatInt(ids["Other"], 10)}

	// a dry run changes nothing
	out, err = runCmd(t, libraryMergeCmd, func() error { return libraryMergeCmd.RunE(libraryMergeCmd, args) })
	if err != nil || !strings.Contains(out, "--apply") {
		t.Fatalf("merge dry run: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(root, "Other", "Chapter 2.cbz")); err != nil {
		t.Fatal("the dry run moved files")
	}

	libraryMergeArgs.Apply = true
	t.Cleanup(func() { libraryMergeArgs.Apply = false })
	out, err = runCmd(t, libraryMergeCmd, func() error { return libraryMergeCmd.RunE(libraryMergeCmd, args) })
	if err != nil || !strings.Contains(out, "merged") {
		t.Fatalf("merge: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(root, "Keep", "Chapter 2.cbz")); err != nil {
		t.Errorf("chapter 2 not moved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Other")); !os.IsNotExist(err) {
		t.Errorf("other's folder left: %v", err)
	}

	libraryTrashEmptyArgs.Yes = true
	t.Cleanup(func() { libraryTrashEmptyArgs.Yes = false })
	out, err = runCmd(t, libraryTrashEmptyCmd, func() error { return libraryTrashEmptyCmd.RunE(libraryTrashEmptyCmd, nil) })
	if err != nil || !strings.Contains(out, "can't be undone after it") || !strings.Contains(out, "merge") {
		t.Fatalf("trash empty: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(root, library.TrashDir)); !os.IsNotExist(err) {
		t.Errorf("trash left: %v", err)
	}

	// the merge's undo needs the trash: blocked now
	undoer, err := client.LibraryUndoer()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := undoer.Plan(0)
	if err != nil || plan.Op.Kind != "merge" || len(plan.Blocked()) == 0 {
		t.Errorf("undo plan %+v, blocked %v, %v", plan.Op, plan.Blocked(), err)
	}

	// a missing folder is listed, removed only when asked (not without a terminal)
	if err := os.RemoveAll(filepath.Join(root, "Keep")); err != nil {
		t.Fatal(err)
	}
	out, err = runCmd(t, libraryScanCmd, func() error { return libraryScanCmd.RunE(libraryScanCmd, nil) })
	if err != nil || !strings.Contains(out, "1 mangas weren't found") || strings.Contains(out, "removed\n") {
		t.Fatalf("scan with a missing folder: %v\n%s", err, out)
	}
	if _, ok, err := lib.MangaByID(ids["Keep"]); err != nil || !ok {
		t.Errorf("removed without asking: %v, %v", ok, err)
	}
}

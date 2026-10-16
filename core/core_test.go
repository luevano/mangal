package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

// testCore is a Core over fresh databases, a temp download directory and the OS files.
func testCore(t *testing.T) (*Core, string) {
	t.Helper()
	dir := t.TempDir()
	data, err := storage.OpenData(filepath.Join(dir, storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { data.Close() })
	cache, err := storage.OpenCache(filepath.Join(dir, storage.FilenameCache))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cache.Close() })
	root := filepath.Join(dir, "downloads")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	c, err := New(context.Background(), Env{
		Storage:      storage.Storage{Data: data, Cache: cache},
		FS:           afero.NewOsFs(),
		Root:         func() string { return root },
		ProviderDirs: func() bool { return false },
		Modes:        func() library.Modes { return library.DefaultModes },
		BackupsDir:   filepath.Join(dir, "backups"),
		Backups:      10,
		Loaders:      func() ([]libmangal.ProviderLoader, error) { return nil, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return c, root
}

// addManga downloads the chapters of the provider's manga into root/dir.
func addManga(t *testing.T, c *Core, dir, provider, id, title string, chapters ...float32) storage.Manga {
	t.Helper()
	root := c.env.Root()
	lib := c.env.Storage.Library()
	if err := lib.RememberMangaDir(filepath.Join(root, filepath.Dir(dir)), filepath.Base(dir), libmangal.MangaDirInfo{Provider: provider, Manga: mangadata.MangaInfo{ID: id, Title: title}}); err != nil {
		t.Fatal(err)
	}
	for _, n := range chapters {
		download(t, c, dir, provider, id, title, n)
	}
	m, ok, err := lib.MangaBySource(provider, id)
	if err != nil || !ok {
		t.Fatalf("manga %s: %t %v", title, ok, err)
	}
	return m
}

func download(t *testing.T, c *Core, dir, provider, id, title string, n float32) {
	t.Helper()
	root := c.env.Root()
	path := filepath.Join(root, dir, fmt.Sprintf("[%06.1f] Chapter %g.cbz", n, n))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("cbz"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := c.env.Storage.Library().RecordDownload(filepath.Join(root, filepath.Dir(dir)), filepath.Base(dir), storage.Download{
		Path: path, Provider: provider, MangaID: id, MangaTitle: title, Chapter: n, Format: "CBZ", Downloaded: time.Unix(1, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// undoLast plans and applies the undo of the last operation.
func undoLast(t *testing.T, c *Core) OpResult {
	t.Helper()
	p, err := c.Changes.Plan(context.Background(), UndoReq{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Blocked) != 0 {
		t.Fatalf("undo blocked: %v\n%s", p.Blocked, strings.Join(p.Lines, "\n"))
	}
	res, err := c.Changes.ApplyPlan(context.Background(), p, ApplyOpts{})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestMerge(t *testing.T) {
	c, root := testCore(t)
	ctx := context.Background()
	keep := addManga(t, c, "Berserk", "mango-mangadex", "b", "Berserk", 1, 2)
	other := addManga(t, c, "Berserk (WC)", "mango-weebcentral", "bw", "Berserk", 2, 3)

	p, err := c.Changes.Plan(ctx, MergeReq{Keep: keep.ID, Other: other.ID})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		fmt.Sprintf("keep %q (%s), merge %q (%s) into it:", "Berserk", filepath.Join(root, "Berserk"), "Berserk", filepath.Join(root, "Berserk (WC)")),
		"  move   [0003.0] Chapter 3.cbz",
		`  trash  [0002.0] Chapter 2.cbz (already in "Berserk")`,
	}
	if strings.Join(p.Lines, "\n") != strings.Join(want, "\n") || p.Kind != KindMerge || !strings.HasPrefix(p.Hash, "sha256:") {
		t.Fatalf("plan %+v", p)
	}
	again, _ := c.Changes.Plan(ctx, MergeReq{Keep: keep.ID, Other: other.ID})
	if again.Hash != p.Hash {
		t.Error("same plan, another hash")
	}
	res, err := c.Changes.ApplyPlan(ctx, p, ApplyOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if res.OpID == 0 || res.Snapshot == "" || res.Summary != fmt.Sprintf("merge %d \"Berserk\" into %d \"Berserk\"", other.ID, keep.ID) {
		t.Errorf("result %+v", res)
	}
	if !exists(filepath.Join(root, "Berserk", "[0003.0] Chapter 3.cbz")) || exists(filepath.Join(root, "Berserk (WC)")) {
		t.Fatal("not merged")
	}
	if _, err := c.Library.Manga(t.Context(), other.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("other still there: %v", err)
	}

	undoLast(t, c)
	if !exists(filepath.Join(root, "Berserk (WC)", "[0003.0] Chapter 3.cbz")) || exists(filepath.Join(root, "Berserk", "[0003.0] Chapter 3.cbz")) {
		t.Fatal("not undone")
	}
	if _, err := c.Library.Manga(t.Context(), other.ID); err != nil {
		t.Errorf("other not back: %v", err)
	}
}

// a plan the library changed under isn't applied
func TestPlanChanged(t *testing.T) {
	c, _ := testCore(t)
	ctx := context.Background()
	keep := addManga(t, c, "A", "p", "a", "A", 1)
	other := addManga(t, c, "B", "p", "b", "B", 2)
	p, err := c.Changes.Plan(ctx, MergeReq{Keep: keep.ID, Other: other.ID})
	if err != nil {
		t.Fatal(err)
	}
	download(t, c, "B", "p", "b", "B", 3)
	_, err = c.Changes.ApplyPlan(ctx, p, ApplyOpts{})
	var changed *PlanChangedError
	if !errors.As(err, &changed) || !errors.Is(err, ErrPlanChanged) || len(changed.Plan.Lines) != len(p.Lines)+1 {
		t.Fatalf("applied a stale plan: %v", err)
	}
	// stateless: the hash confirmed
	if _, err := c.Changes.Apply(ctx, MergeReq{Keep: keep.ID, Other: other.ID}, ApplyOpts{Expect: p.Hash}); !errors.Is(err, ErrPlanChanged) {
		t.Fatalf("applied with a stale hash: %v", err)
	}
	if _, err := c.Changes.Apply(ctx, MergeReq{Keep: keep.ID, Other: other.ID}, ApplyOpts{Expect: changed.Plan.Hash}); err != nil {
		t.Fatal(err)
	}
	// planned again only when the library changed: the same plan holds otherwise
	if _, err := c.Changes.Plan(ctx, MergeReq{Keep: keep.ID, Other: other.ID}); !errors.Is(err, ErrNotFound) {
		t.Errorf("merged manga still there: %v", err)
	}
}

func TestDelete(t *testing.T) {
	c, root := testCore(t)
	ctx := context.Background()
	m := addManga(t, c, "Vagabond", "p", "v", "Vagabond", 1, 2)

	// a chapter, by number
	p, err := c.Changes.Plan(ctx, DeleteReq{Manga: m.ID, Chapter: "2"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Summary != fmt.Sprintf("delete chapter 2 of %d \"Vagabond\"", m.ID) || p.Lines[0] != "trash Vagabond/[0002.0] Chapter 2.cbz" {
		t.Fatalf("plan %+v", p)
	}
	if _, err := c.Changes.ApplyPlan(ctx, p, ApplyOpts{}); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(root, "Vagabond", "[0002.0] Chapter 2.cbz")) {
		t.Fatal("chapter not deleted")
	}
	if _, err := c.Changes.Plan(ctx, DeleteReq{Manga: m.ID, Chapter: "9"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("chapter 9: %v", err)
	}

	// the manga
	p, err = c.Changes.Plan(ctx, DeleteReq{Manga: m.ID})
	if err != nil {
		t.Fatal(err)
	}
	if p.Lines[0] != "trash Vagabond (1 chapter)" {
		t.Errorf("lines %q", p.Lines)
	}
	if _, err := c.Changes.ApplyPlan(ctx, p, ApplyOpts{}); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(root, "Vagabond")) {
		t.Fatal("not deleted")
	}
	trash, _ := c.Library.Trash(t.Context())
	if files := 0; len(trash) != 0 {
		for _, b := range trash {
			files += b.Files
		}
		if files != 2 {
			t.Errorf("%d files in the trash", files)
		}
	} else {
		t.Error("nothing in the trash")
	}
	undoLast(t, c)
	if !exists(filepath.Join(root, "Vagabond", "[0001.0] Chapter 1.cbz")) {
		t.Fatal("delete not undone")
	}

	// its folder gone: only forgotten
	if err := os.RemoveAll(filepath.Join(root, "Vagabond")); err != nil {
		t.Fatal(err)
	}
	p, err = c.Changes.Plan(ctx, DeleteReq{Manga: m.ID})
	if err != nil || !p.Detail.(DeletePlan).Missing {
		t.Fatalf("%+v %v", p, err)
	}
	if res, err := c.Changes.ApplyPlan(ctx, p, ApplyOpts{}); err != nil || res.Summary != fmt.Sprintf("remove missing %d \"Vagabond\"", m.ID) {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := c.Library.Manga(t.Context(), m.ID); !errors.Is(err, ErrNotFound) {
		t.Error("not forgotten")
	}
}

func TestForget(t *testing.T) {
	c, _ := testCore(t)
	ctx := context.Background()
	a := addManga(t, c, "A", "p", "a", "A", 1)
	b := addManga(t, c, "B", "p", "b", "B", 1)
	res, err := c.Changes.Apply(ctx, ForgetReq{Mangas: []int64{a.ID, b.ID}}, ApplyOpts{})
	if err != nil || res.Summary != "remove 2 missing mangas" || res.OpID == 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if list, _ := c.Library.Mangas(ctx, MangasOptions{}); len(list.Mangas) != 0 {
		t.Fatalf("%d left", len(list.Mangas))
	}
	undoLast(t, c)
	if list, _ := c.Library.Mangas(ctx, MangasOptions{}); len(list.Mangas) != 2 {
		t.Fatalf("%d back", len(list.Mangas))
	}
	if _, err := c.Changes.Plan(ctx, ForgetReq{}); !errors.Is(err, ErrInvalid) {
		t.Errorf("nothing to forget: %v", err)
	}
}

// emptying the trash can't be undone, nor the operations that put files there after it
func TestEmptyTrash(t *testing.T) {
	c, root := testCore(t)
	ctx := context.Background()
	if p, err := c.Changes.Plan(ctx, EmptyTrashReq{}); err != nil || !p.Nothing {
		t.Fatalf("empty: %+v %v", p, err)
	}
	if _, err := c.Changes.Apply(ctx, EmptyTrashReq{}, ApplyOpts{}); !errors.Is(err, ErrNothingToDo) {
		t.Errorf("applied nothing: %v", err)
	}
	m := addManga(t, c, "A", "p", "a", "A", 1)
	if _, err := c.Changes.Apply(ctx, DeleteReq{Manga: m.ID}, ApplyOpts{}); err != nil {
		t.Fatal(err)
	}
	p, err := c.Changes.Plan(ctx, EmptyTrashReq{})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Irreversible || len(p.Detail.(EmptyTrashPlan).Blocked) != 1 || p.Lines[0] != "these operations can't be undone after it:" ||
		p.Lines[len(p.Lines)-1] != "delete 1 file (3 B) in the trash for good" {
		t.Fatalf("plan %+v", p)
	}
	res, err := c.Changes.ApplyPlan(ctx, p, ApplyOpts{})
	if err != nil || res.Snapshot == "" || res.OpID != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if trash, _ := c.Library.Trash(t.Context()); len(trash) != 0 || exists(filepath.Join(root, ".mangal-trash", "x")) {
		t.Fatal("not emptied")
	}
	up, err := c.Changes.Plan(ctx, UndoReq{})
	if err != nil {
		t.Fatal(err)
	}
	if len(up.Blocked) == 0 {
		t.Fatalf("the delete's undo isn't blocked: %q", up.Lines)
	}
	if _, err := c.Changes.ApplyPlan(ctx, up, ApplyOpts{}); !errors.Is(err, ErrRefused) {
		t.Fatalf("undone without force: %v", err)
	}
	if !up.Forceable {
		t.Fatal("not forceable")
	}
	res, err = c.Changes.ApplyPlan(ctx, up, ApplyOpts{Force: true})
	if err != nil || len(res.Lines) == 0 || !res.Lines[0].Warn {
		t.Fatalf("forced: %+v %v", res, err)
	}
}

func TestHistoryClear(t *testing.T) {
	c, _ := testCore(t)
	ctx := context.Background()
	history := c.env.Storage.History()
	for _, r := range []storage.Read{{Provider: "p", MangaID: "a", Chapter: 1}, {Provider: "p", MangaID: "a", Chapter: 2}, {Provider: "q", MangaID: "b", Chapter: 1}} {
		if err := history.Record(r); err != nil {
			t.Fatal(err)
		}
	}
	p, err := c.Changes.Plan(ctx, HistoryClearReq{Provider: "p"})
	if err != nil || p.Lines[0] != "forget 2 read chapters" || p.Summary != "forget the read chapters of p" {
		t.Fatalf("%+v %v", p, err)
	}
	var lines []string
	res, err := c.Changes.ApplyPlan(ctx, p, ApplyOpts{OnLine: func(l Line) { lines = append(lines, l.Text) }})
	if err != nil || res.Detail.(int64) != 2 || strings.Join(lines, "") != "forgot 2 read chapters" {
		t.Fatalf("%+v %v %q", res, err, lines)
	}
	if reads, _ := c.Library.History(t.Context(), "", ""); len(reads) != 1 {
		t.Fatalf("%d reads left", len(reads))
	}
	undoLast(t, c)
	if reads, _ := c.Library.History(t.Context(), "", ""); len(reads) != 3 {
		t.Fatalf("%d reads back", len(reads))
	}
	if _, err := c.Changes.Plan(ctx, HistoryClearReq{MangaID: "a"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("manga without provider: %v", err)
	}
}

func TestUndoNothing(t *testing.T) {
	c, _ := testCore(t)
	if _, err := c.Changes.Plan(context.Background(), UndoReq{}); !errors.Is(err, library.ErrNothingToUndo) {
		t.Errorf("%v", err)
	}
}

// a change waits for the library's lock, or is busy
func TestLockContention(t *testing.T) {
	c, _ := testCore(t)
	ctx := context.Background()
	m := addManga(t, c, "A", "p", "a", "A", 1)
	// another holder (as another process would be)
	_, unlock, err := client.LockLibrary(ctx, c.env.Storage.Watches(), "rename", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Changes.Apply(ctx, ForgetReq{Mangas: []int64{m.ID}}, ApplyOpts{NoWait: true})
	var busy *client.LibraryBusyError
	if !errors.Is(err, ErrBusy) || !errors.As(err, &busy) || busy.Holder.Purpose != "rename" {
		t.Fatalf("not busy: %v", err)
	}
	waiting := make(chan client.LibraryBusy, 1)
	done := make(chan error, 1)
	go func() {
		_, err := c.Changes.Apply(ctx, ForgetReq{Mangas: []int64{m.ID}}, ApplyOpts{OnWait: func(b client.LibraryBusy) { waiting <- b }})
		done <- err
	}()
	if b := <-waiting; b.Holder.Purpose != "rename" {
		t.Errorf("waited for %+v", b.Holder)
	}
	select {
	case err := <-done:
		t.Fatalf("didn't wait: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := c.Library.Manga(t.Context(), m.ID); !errors.Is(err, ErrNotFound) {
		t.Error("not applied after the wait")
	}
}

func TestScan(t *testing.T) {
	c, root := testCore(t)
	ctx := context.Background()
	if err := os.MkdirAll(filepath.Join(root, "Berserk"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Berserk", "Chapter 1.cbz"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := c.Library.Scan(ctx, ScanOptions{})
	if err != nil || rep.Result.New != 1 || rep.Root != root || rep.Checked {
		t.Fatalf("%+v %v", rep, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.Library.Scan(cancelled, ScanOptions{}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
}

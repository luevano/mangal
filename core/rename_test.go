package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/path"
	"github.com/spf13/afero"
)

// markFolder writes the folder's .libmangal.json, with the AniList id.
func markFolder(t *testing.T, dir, provider, id, title, al string) {
	t.Helper()
	info := libmangal.MangaDirInfo{Provider: provider, Manga: mangadata.MangaInfo{ID: id, Title: title}}
	if al != "" {
		info.MetadataIDs = []metadata.ID{{Raw: al, Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}}
	}
	if err := libmangal.WriteMangaDirInfo(afero.NewOsFs(), dir, info, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRename(t *testing.T) {
	c, root := testLinkCore(t)
	ctx := context.Background()
	env := c.env
	env.ProviderDirs = func() bool { return false }
	c, _ = New(ctx, env)
	addManga(t, c, "Berserk", "p", "b", "Berserk", 1)
	addManga(t, c, "Other", "p", "o", "Other", 1)
	markFolder(t, filepath.Join(root, "Berserk"), "p", "b", "Berserk", "30002")
	markFolder(t, filepath.Join(root, "Other"), "p", "o", "Other", "")

	p, err := c.Changes.Plan(ctx, RenameReq{})
	if err != nil {
		t.Fatal(err)
	}
	want := "Berserk -> Berserk (1989) [alid-30002]"
	if strings.Join(p.Lines, "\n") != want || p.Summary != "rename 1 folders" {
		t.Fatalf("plan %q %q", p.Lines, p.Summary)
	}
	// only the selected ones: none
	if p, err := c.Changes.Plan(ctx, RenameReq{Only: []string{}}); err != nil || !p.Nothing {
		t.Errorf("nothing selected: %+v %v", p, err)
	}
	// a finding's paths
	if p, _ := c.Changes.Plan(ctx, RenameReq{Paths: []string{filepath.Join(root, "Other")}}); !p.Nothing {
		t.Errorf("other's paths: %q", p.Lines)
	}
	var lines []string
	res, err := c.Changes.ApplyPlan(ctx, p, ApplyOpts{OnLine: func(l Line) { lines = append(lines, l.Text) }})
	if err != nil {
		t.Fatal(err)
	}
	if done := res.Detail.(client.RenameResult); done.Folders != 1 || strings.Join(lines, "\n") != want || res.Summary != "rename 1 folders" {
		t.Errorf("result %+v %q", res, lines)
	}
	if !exists(filepath.Join(root, "Berserk (1989) [alid-30002]", "[0001.0] Chapter 1.cbz")) {
		t.Fatal("not renamed")
	}
	undoLast(t, c)
	if !exists(filepath.Join(root, "Berserk")) {
		t.Fatal("not undone")
	}

	// cancelled: nothing renamed
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.Changes.Apply(cancelled, RenameReq{}, ApplyOpts{}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
	if !exists(filepath.Join(root, "Berserk")) {
		t.Error("renamed after all")
	}
}

// changes that work on the folders refuse a download path that isn't there (a drive not
// mounted) instead of planning on nothing
func TestNoDownloads(t *testing.T) {
	c, root := testCore(t)
	m := addManga(t, c, "A", "p", "a", "A", 1)
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	for _, ch := range []Change{RenameReq{}, MarkReq{}, MigrateReq{}, MergeReq{Keep: m.ID, Other: m.ID}, DeleteReq{Manga: m.ID}} {
		if _, err := c.Changes.Plan(context.Background(), ch); !errors.Is(err, path.ErrNoDownloads) {
			t.Errorf("%s: %v", ch.Kind(), err)
		}
	}
}

func TestMark(t *testing.T) {
	c, root := testCore(t)
	ctx := context.Background()
	m := addManga(t, c, "Berserk", "p", "b", "Berserk", 1)
	p, err := c.Changes.Plan(ctx, MarkReq{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Lines) != 1 || !strings.HasPrefix(p.Lines[0], "would mark "+filepath.Join(root, "Berserk")+" as p b") {
		t.Fatalf("plan %q", p.Lines)
	}
	var lines []string
	res, err := c.Changes.ApplyPlan(ctx, p, ApplyOpts{OnLine: func(l Line) { lines = append(lines, l.Text) }})
	if err != nil || len(res.Detail.(library.MarkResult).Marked) != 1 || len(lines) != 1 || !strings.HasPrefix(lines[0], "marked ") {
		t.Fatalf("%+v %v %q", res, err, lines)
	}
	if _, err := libmangal.ReadMangaDirInfo(afero.NewOsFs(), filepath.Join(root, "Berserk")); err != nil {
		t.Fatal("not marked")
	}
	if p, _ := c.Changes.Plan(ctx, MarkReq{}); !p.Nothing {
		t.Errorf("marked again: %q", p.Lines)
	}
	// one manga: its marker agrees
	if p, _ := c.Changes.Plan(ctx, MarkReq{Mangas: []int64{m.ID}}); !p.Nothing || !strings.Contains(p.Lines[0], "nothing to do") {
		t.Errorf("agrees: %q", p.Lines)
	}
	undoLast(t, c)
	if _, err := os.Stat(filepath.Join(root, "Berserk", libmangal.FilenameMangaDirInfo)); err == nil {
		t.Error("marker left")
	}
	// a broken one is replaced for that manga only
	if err := os.WriteFile(filepath.Join(root, "Berserk", libmangal.FilenameMangaDirInfo), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p, _ := c.Changes.Plan(ctx, MarkReq{}); !p.Nothing || len(p.Detail.(MarkDetail).Result.Broken) != 1 {
		t.Errorf("broken, all: %+v", p.Detail)
	}
	if _, err := c.Changes.Apply(ctx, MarkReq{Mangas: []int64{m.ID}}, ApplyOpts{}); err != nil {
		t.Fatal(err)
	}
	if _, err := libmangal.ReadMangaDirInfo(afero.NewOsFs(), filepath.Join(root, "Berserk")); err != nil {
		t.Fatal("broken marker not replaced")
	}
}

func TestMigrateNothing(t *testing.T) {
	c, _ := testCore(t)
	p, err := c.Changes.Plan(context.Background(), MigrateReq{})
	if err != nil || !p.Nothing {
		t.Fatalf("%+v %v", p, err)
	}
}

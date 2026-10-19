package core

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
)

const testComicInfo = `<?xml version="1.0" encoding="UTF-8"?>
<ComicInfo xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:xsd="http://www.w3.org/2001/XMLSchema">
  <Title>Chapter 1</Title>
  <Series>Dark Gathering</Series>
  <Number>1</Number>
  <Notes>kept &amp; as is</Notes>
  <Unknown attr="x">another tool's</Unknown>
</ComicInfo>`

// writeCBZ writes a CBZ with a page (stored, not compressed) and the ComicInfo.xml, if any.
func writeCBZ(t *testing.T, path, comicInfo string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.CreateHeader(&zip.FileHeader{Name: "0001.jpg", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("page one"))
	if comicInfo != "" {
		w, err := zw.Create("ComicInfo.xml")
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(comicInfo))
	}
	zw.SetComment("a comment")
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readZip(t *testing.T, path string) (*zip.ReadCloser, map[string]string) {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { zr.Close() })
	files := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(rc)
		rc.Close()
		files[f.Name] = string(data)
	}
	return zr, files
}

func TestRetitle(t *testing.T) {
	c, root := testCore(t)
	ctx := context.Background()
	env := c.env
	env.ChapterName = func(_ libmangal.ProviderInfo, ch mangadata.Chapter) string {
		return fmt.Sprintf("[%06.1f] %s", ch.Info().Number, ch.Info().Title)
	}
	c, _ = New(ctx, env)
	m := addManga(t, c, "Dark Gathering", "p", "dg", "Dark Gathering", 1, 2, 3)
	dir := filepath.Join(root, "Dark Gathering")
	ch1, ch2 := filepath.Join(dir, "[0001.0] Chapter 1.cbz"), filepath.Join(dir, "[0002.0] Chapter 2.cbz")
	writeCBZ(t, ch1, testComicInfo)
	writeCBZ(t, ch2, "")

	req := RetitleReq{Manga: m.ID, Provider: libmangal.ProviderInfo{ID: "mango-mangadex"}, From: mangadata.MangaInfo{Title: "Dark Gathering"},
		Titles: map[float32]string{1: "The Beginning", 2: "Tom & <Jerry>", 4: "Not downloaded"}}
	p, err := c.Changes.Plan(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	want := `1 (CBZ): "" -> "The Beginning", ComicInfo.xml too
  [0001.0] Chapter 1.cbz -> [0001.0] The Beginning.cbz
2 (CBZ): "" -> "Tom & <Jerry>"
  [0002.0] Chapter 2.cbz -> [0002.0] Tom & <Jerry>.cbz
no title for chapters 3`
	if got := strings.Join(p.Lines, "\n"); got != want {
		t.Fatalf("plan:\n%s\nwant:\n%s", got, want)
	}
	res, err := c.Changes.ApplyPlan(ctx, p, ApplyOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Detail != 2 || len(res.Lines) != 2 || res.Lines[0].Warn {
		t.Fatalf("result %+v", res)
	}
	new1, new2 := filepath.Join(dir, "[0001.0] The Beginning.cbz"), filepath.Join(dir, "[0002.0] Tom & <Jerry>.cbz")
	if exists(ch1) || exists(ch2) || !exists(new1) || !exists(new2) {
		t.Fatal("not renamed")
	}
	// the new file: the title changed, the rest as it was
	zr, files := readZip(t, new1)
	if wantCI := strings.Replace(testComicInfo, "<Title>Chapter 1</Title>", "<Title>The Beginning</Title>", 1); files["ComicInfo.xml"] != wantCI {
		t.Errorf("ComicInfo.xml:\n%s", files["ComicInfo.xml"])
	}
	if files["0001.jpg"] != "page one" || zr.File[0].Name != "0001.jpg" || zr.File[0].Method != zip.Store || zr.Comment != "a comment" || len(zr.File) != 2 {
		t.Errorf("entries %v, comment %q", files, zr.Comment)
	}
	// the original in the trash
	trashed, _ := filepath.Glob(filepath.Join(root, ".mangal-trash", "*", "Dark Gathering", "*.cbz"))
	if len(trashed) != 1 || filepath.Base(trashed[0]) != "[0001.0] Chapter 1.cbz" {
		t.Errorf("original not in the trash: %v", trashed)
	}
	// the records
	dls, err := c.env.Storage.Library().Downloads(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dls[0].Path != new1 || dls[0].ChapterTitle != "The Beginning" || dls[1].Path != new2 || dls[2].ChapterTitle != "" {
		t.Errorf("downloads %+v", dls)
	}
	var name string
	if err := c.env.Storage.Data.QueryRow(`SELECT name FROM library_chapters WHERE manga = ? AND number = 1`, m.ID).Scan(&name); err != nil || name != "The Beginning" {
		t.Errorf("library chapter %q %v", name, err)
	}
	if p, _ := c.Changes.Plan(ctx, req); !p.Nothing {
		t.Errorf("again: %q", p.Lines)
	}

	undoLast(t, c)
	if !exists(ch1) || !exists(ch2) || exists(new1) || exists(new2) {
		t.Fatal("not undone")
	}
	if _, files := readZip(t, ch1); files["ComicInfo.xml"] != testComicInfo {
		t.Error("the original changed")
	}
	if dls, _ := c.env.Storage.Library().Downloads(m.ID); dls[0].Path != ch1 || dls[0].ChapterTitle != "" {
		t.Errorf("downloads after undo %+v", dls)
	}
}

// a name taken is skipped, the others are done
func TestRetitleTaken(t *testing.T) {
	c, root := testCore(t)
	ctx := context.Background()
	env := c.env
	env.ChapterName = func(_ libmangal.ProviderInfo, ch mangadata.Chapter) string { return ch.Info().Title }
	c, _ = New(ctx, env)
	m := addManga(t, c, "M", "p", "m", "M", 1, 2)
	if err := os.WriteFile(filepath.Join(root, "M", "Taken.cbz"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := c.Changes.Plan(ctx, RetitleReq{Manga: m.ID, Provider: libmangal.ProviderInfo{ID: "p"}, Titles: map[float32]string{1: "Taken", 2: "Free"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Lines[0], "skipped: a file of that name exists") || p.Summary != `retitle 1 chapters of 1 "M" from p ""` {
		t.Fatalf("plan %q %q", p.Lines, p.Summary)
	}
	if _, err := c.Changes.ApplyPlan(ctx, p, ApplyOpts{}); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(root, "M", "[0001.0] Chapter 1.cbz")) || !exists(filepath.Join(root, "M", "Free.cbz")) {
		t.Error("not as planned")
	}
}

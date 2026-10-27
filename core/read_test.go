package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/health"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/storage"
)

func TestLibraryRead(t *testing.T) {
	c, root := testCore(t)
	ctx := t.Context()
	a := addManga(t, c, "A", "p", "a", "A", 1, 2)
	b := addManga(t, c, "B", "p", "b", "B", 1)
	if err := os.RemoveAll(filepath.Join(root, "B")); err != nil {
		t.Fatal(err)
	}
	list, err := c.Library.Mangas(ctx, MangasOptions{Missing: true})
	if err != nil || len(list.Mangas) != 2 || !list.IsMissing(b.ID) || list.IsMissing(a.ID) {
		t.Fatalf("%+v %v", list, err)
	}
	if err := c.env.Storage.History().Of(storage.DefaultUser).Record(storage.Read{Provider: "p", MangaID: "a", MangaTitle: "A", Chapter: 2}); err != nil {
		t.Fatal(err)
	}
	files, err := c.Library.ChapterFiles(ctx, ChapterFilesReq{Provider: libmangal.ProviderInfo{ID: "p"}, MangaID: "a"})
	if err != nil || !files.Found || files.Manga.ID != a.ID || len(files.Downloads) != 2 || !slices.Equal(files.Read, []float32{2}) || len(files.Errors) != 0 {
		t.Fatalf("%+v %v", files, err)
	}
	if files, _ := c.Library.ChapterFiles(ctx, ChapterFilesReq{Provider: libmangal.ProviderInfo{ID: "p"}, MangaID: "x"}); files.Found {
		t.Errorf("found %+v", files)
	}
	if m, ok, err := c.Library.MangaBySource(ctx, "p", "a"); !ok || err != nil || m.ID != a.ID {
		t.Errorf("by source: %v %t %v", m, ok, err)
	}

	// a chapter added to the folder: the manga's scan records it
	download(t, c, "A", "p", "a", "A", 3)
	path := filepath.Join(root, "A", "[0004.0] Chapter 4.cbz")
	if err := os.WriteFile(path, []byte("cbz"), 0o644); err != nil {
		t.Fatal(err)
	}
	scan, err := c.Library.ScanManga(ctx, a.ID)
	if err != nil || scan.Manga.ID != a.ID {
		t.Fatalf("%+v %v", scan, err)
	}
	if dls, _ := c.Library.Downloads(ctx, a.ID); len(dls) != 4 {
		t.Errorf("%d downloads after the scan", len(dls))
	}
	if _, err := c.Library.ScanManga(ctx, 99); !errors.Is(err, ErrNotFound) {
		t.Errorf("scan of no manga: %v", err)
	}
}

// gapsChecker is a health checker of the library's gaps only.
func gapsChecker(c *Core) func(watch.Manager) (health.Checker, error) {
	return func(m watch.Manager) (health.Checker, error) {
		return health.Checker{Library: c.env.Storage.Library(), Watches: m, Ignores: storage.HealthIgnores{DB: c.env.Storage.Data}.List}, nil
	}
}

type noWatches struct{ watch.Manager }

func (noWatches) List(context.Context) ([]watch.Watch, error) { return nil, nil }

func TestHealthDismiss(t *testing.T) {
	c, _ := testCore(t)
	ctx := t.Context()
	m := addManga(t, c, "A", "p", "a", "A", 1, 2, 4)
	env := c.env
	env.Checker = gapsChecker(c)
	env.Watches = func(context.Context) (watch.Manager, error) { return noWatches{}, nil }
	c, err := New(ctx, env)
	if err != nil {
		t.Fatal(err)
	}
	var phases []string
	report, err := c.Health.Report(WithProgress(ctx, func(p library.Progress) { phases = append(phases, p.Phase) }), health.Options{Only: []string{"G1"}})
	if err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("G1/manga/%d", m.ID)
	if _, ok := report.Find(key); !ok {
		t.Fatalf("no %s in %+v", key, report.Findings)
	}
	if len(phases) == 0 {
		t.Error("no progress")
	}
	dismissed, err := c.Health.Dismiss(ctx, DismissReq{Keys: []string{key}})
	if err != nil || len(dismissed) != 1 || dismissed[0].Key != key {
		t.Fatalf("%+v %v", dismissed, err)
	}
	if report, _ := c.Health.Report(ctx, health.Options{Only: []string{"G1"}}); len(report.Findings) != 0 {
		t.Errorf("dismissed, still %+v", report.Findings)
	}
	if _, err := c.Health.Dismiss(ctx, DismissReq{Keys: []string{"G1/manga/99"}}); !errors.Is(err, ErrNotFound) {
		t.Errorf("dismiss of no finding: %v", err)
	}
	if err := c.Health.Undismiss(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := c.Health.Undismiss(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Errorf("undismiss twice: %v", err)
	}
	// a known finding is dismissed as it is
	f := health.Finding{Key: key, Message: "known"}
	if dismissed, err := c.Health.Dismiss(ctx, DismissReq{Keys: []string{key}, Known: []health.Finding{f}}); err != nil || dismissed[0].Message != "known" {
		t.Errorf("%+v %v", dismissed, err)
	}
	if f, err := c.Finding(ctx, key, false); err != nil || f.Key != key {
		t.Errorf("finding: %+v %v", f, err)
	}
}

func TestLogs(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"2026-10-12.log":   "2026-10-12T10:00:00.000 INFO  old one\n2026-10-12T11:00:00.000 ERROR old failure\n",
		"2026-10-13.log":   "2026-10-13T09:00:00.000 DEBUG page 1\n2026-10-13T10:00:00.000 WARN  MangaDex timeout\n",
		"2026-10-13.1.log": "2026-10-13T14:00:00.000 INFO  downloaded Foo\n",
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	logs := NewLogs(func() string { return dir })
	ctx := t.Context()
	got := func(q LogQuery) ([]string, error) {
		var msgs []string
		err := logs.Entries(ctx, q, func(e log.Entry) error {
			msgs = append(msgs, e.Message)
			return nil
		})
		return msgs, err
	}
	if fs, err := logs.Files(ctx); err != nil || len(fs) != 3 {
		t.Fatalf("%v %v", fs, err)
	}
	for _, tt := range []struct {
		q    LogQuery
		want []string
	}{
		{LogQuery{}, []string{"old one", "old failure", "page 1", "MangaDex timeout", "downloaded Foo"}},
		{LogQuery{Level: "warn"}, []string{"old failure", "MangaDex timeout"}},
		{LogQuery{Grep: "mangadex"}, []string{"MangaDex timeout"}},
		{LogQuery{Grep: "mangadex", CaseSensitive: true}, nil},
		{LogQuery{Limit: 2}, []string{"MangaDex timeout", "downloaded Foo"}},
		{LogQuery{Files: []string{"2026-10-12.log"}}, []string{"old one", "old failure"}},
		{LogQuery{Since: time.Date(2026, 10, 13, 12, 0, 0, 0, time.Local)}, []string{"downloaded Foo"}},
	} {
		msgs, err := got(tt.q)
		if err != nil || !slices.Equal(msgs, tt.want) {
			t.Errorf("%+v: %q %v", tt.q, msgs, err)
		}
	}
	if _, err := got(LogQuery{Files: []string{"nope"}}); !errors.Is(err, ErrNotFound) {
		t.Errorf("no file: %v", err)
	}
	if _, err := got(LogQuery{Level: "loud"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad level: %v", err)
	}
	if _, err := got(LogQuery{Limit: 10_000_000_000}); !errors.Is(err, ErrInvalid) {
		t.Errorf("huge limit: %v", err)
	}
}

func TestConfigEntries(t *testing.T) {
	cfg := NewConfig()
	ctx := t.Context()
	if _, err := cfg.Get(ctx, "no.such.key"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown key: %v", err)
	}
	entries, err := cfg.List(ctx)
	if err != nil || len(entries) == 0 {
		t.Fatalf("%d %v", len(entries), err)
	}
	for _, e := range entries {
		if e.Kind == "" || e.Description == "" {
			t.Errorf("%+v", e)
		}
		if e.Secret && e.Value != nil && e.Value != "" && e.Value != "••••••" {
			t.Errorf("%s isn't masked: %v", e.Key, e.Value)
		}
	}
}

type testChapter struct {
	mangadata.Chapter
	n float32
}

func (c testChapter) Info() mangadata.ChapterInfo {
	return mangadata.ChapterInfo{Number: c.n, Title: "ch"}
}

func TestJobOf(t *testing.T) {
	j := JobOf(jobs.Job{ID: 3, Kind: jobs.KindDownload, Status: jobs.Failed, Err: errors.New("boom"),
		Results: client.Results{{Chapter: testChapter{n: 1}, Err: errors.New("404")}, {Chapter: testChapter{n: 2}}}})
	if j.Error != "boom" || len(j.Results) != 2 || !j.Results[0].Failed() || !j.Results[1].ToDownload() || !j.Retryable() {
		t.Fatalf("%+v", j)
	}
	data, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	var back Job
	if err := json.Unmarshal(data, &back); err != nil || back.Status != jobs.Failed || back.Kind != jobs.KindDownload || back.Results[0].Error != "404" {
		t.Errorf("%s: %+v %v", data, back, err)
	}
	m := jobs.New(t.Context())
	svc := LocalJobs(m)
	if _, err := svc.Get(t.Context(), 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("no job: %v", err)
	}
	if list, err := svc.List(t.Context()); err != nil || len(list) != 0 {
		t.Errorf("%v %v", list, err)
	}
}

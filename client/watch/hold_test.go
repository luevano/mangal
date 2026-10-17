package watch

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/storage"
)

func TestCoverage(t *testing.T) {
	lib := func(ns ...float32) map[float32]bool {
		have := map[float32]bool{}
		for _, n := range ns {
			have[n] = true
		}
		return have
	}
	upTo := func(n int) []float32 {
		var ns []float32
		for i := 1; i <= n; i++ {
			ns = append(ns, float32(i))
		}
		return ns
	}
	for _, tt := range []struct {
		name   string
		listed []float32
		have   map[float32]bool
		want   string
	}{
		{"no library chapters", []float32{1}, lib(), ""},
		{"all of them", upTo(25), lib(upTo(20)...), ""},
		{"none listed", nil, lib(upTo(20)...), "the provider lists no chapters (library 1-20 (20))"},
		{"only after the library's", []float32{60, 61, 62, 63}, lib(upTo(20)...), "the provider only has chapters after the library's (library 1-20 (20), provider 60-63 (4))"},
		{"continues it but lost the rest", []float32{21, 22}, lib(upTo(20)...), "the provider only has chapters after the library's (library 1-20 (20), provider 21-22 (2))"},
		{"the latest few", []float32{18, 19, 20, 21, 22, 23}, lib(upTo(20)...), "the provider has 3 of the library's 20 chapters (library 1-20 (20), provider 18-23 (6))"},
		{"lost most", upTo(5), lib(upTo(20)...), "the provider has 5 of the library's 20 chapters (library 1-20 (20), provider 1-5 (5))"},
		{"half is enough", upTo(10), lib(upTo(20)...), ""},
	} {
		if got := Coverage(tt.listed, tt.have, 0.5); got != tt.want {
			t.Errorf("%s: %q, want %q", tt.name, got, tt.want)
		}
	}
	if got := Coverage(upTo(5), lib(upTo(20)...), 0); got != "" {
		t.Errorf("min 0: %q", got)
	}
}

func TestContinuation(t *testing.T) {
	m := &fakeManga{info: mangadata.MangaInfo{ID: "m"}}
	chapters := func(ns ...float32) []mangadata.Chapter {
		var chs []mangadata.Chapter
		for _, c := range m.chapters(ns...) {
			chs = append(chs, c)
		}
		return chs
	}
	nums := func(chs []mangadata.Chapter) []float32 {
		var ns []float32
		for _, c := range chs {
			ns = append(ns, c.Info().Number)
		}
		return ns
	}
	for _, tt := range []struct {
		picked     []float32
		kept, held []float32
	}{
		{[]float32{21, 22, 22.5, 23}, []float32{21, 22, 22.5, 23}, nil},
		{[]float32{60, 61}, nil, []float32{60, 61}},
		{[]float32{23, 21, 22, 25}, []float32{21, 22, 23}, []float32{25}},
		// gaps and retries below the last stay
		{[]float32{5, 30}, []float32{5}, []float32{30}},
		{[]float32{20.5, 21}, []float32{20.5, 21}, nil},
	} {
		kept, held := Continuation(chapters(tt.picked...), 20)
		if !slices.Equal(nums(kept), tt.kept) || !slices.Equal(held, tt.held) {
			t.Errorf("%v: kept %v held %v, want %v %v", tt.picked, nums(kept), held, tt.kept, tt.held)
		}
	}
}

func TestCheckCoverage(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.svc.Options.CoverageMin = 0.5
	m := &fakeManga{info: mangadata.MangaInfo{ID: "kingdom", Title: "Kingdom"}}
	src := e.source("mango-mangadex", m)
	e.own(t, "mango-mangadex", m, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10)
	src.chs["kingdom"] = m.chapters(11, 12, 14)
	w, err := e.svc.Add(ctx, AddOptions{Provider: "mango-mangadex", MangaID: "kingdom"})
	if err != nil {
		t.Fatal(err)
	}

	report, err := e.svc.Check(ctx, nil, CheckOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r := report.Results[0]
	if !strings.HasPrefix(r.Coverage, "the provider only has chapters after the library's") || !slices.Equal(numbers(r.Chapters), []float32{11, 12}) || !slices.Equal(r.HeldBack, []float32{14}) {
		t.Errorf("result %+v", r)
	}
	if !strings.Contains(Describe(r, false), "another provider? the provider only has") || !strings.Contains(Describe(r, false), "held back 14") {
		t.Errorf("described %q", Describe(r, false))
	}
	// only suggested: still scheduled
	got, _ := e.svc.Get(ctx, w.ID)
	if got.Coverage != r.Coverage || got.NeedsRelink || !got.Scheduled() {
		t.Errorf("watch %+v", got)
	}

	// it gets the chapters again: the suggestion goes
	src.chs["kingdom"] = m.chapters(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14)
	e.now = e.now.Add(time.Minute)
	report, _ = e.svc.Check(ctx, []int64{w.ID}, CheckOptions{})
	if r := report.Results[0]; r.Coverage != "" || !slices.Equal(numbers(r.Chapters), []float32{13, 14}) {
		t.Errorf("second check %+v", r)
	}
	if got, _ := e.svc.Get(ctx, w.ID); got.Coverage != "" {
		t.Errorf("coverage kept %q", got.Coverage)
	}

	// an exempt provider is never suggested another
	e.svc.Options.CoverageExempt = []string{"mango-mangadex"}
	src.chs["kingdom"] = m.chapters(60)
	report, _ = e.svc.Check(ctx, []int64{w.ID}, CheckOptions{})
	if r := report.Results[0]; r.Coverage != "" || !slices.Equal(numbers(r.Chapters), []float32{60}) {
		t.Errorf("exempt %+v", r)
	}
}

func TestHold(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	m := &fakeManga{info: mangadata.MangaInfo{ID: "hxh", Title: "Hunter x Hunter"}}
	src := e.source("mango-mangadex", m)
	e.own(t, "mango-mangadex", m, 1, 2)
	src.chs["hxh"] = m.chapters(1, 2, 3)
	w, err := e.svc.Add(ctx, AddOptions{Provider: "mango-mangadex", MangaID: "hxh"})
	if err != nil {
		t.Fatal(err)
	}
	manga := w.LibraryManga
	if manga == 0 {
		t.Fatal("no library manga")
	}
	if _, err := e.svc.Hold(ctx, HoldOptions{Watch: w.ID, Mark: "paused"}); err == nil {
		t.Error("held with an unknown mark")
	}
	if _, err := e.svc.Hold(ctx, HoldOptions{Watch: 999, Mark: storage.HoldHiatus}); !errors.Is(err, ErrNotFound) {
		t.Errorf("held a missing watch: %v", err)
	}

	// the last chapters first, then paused, the mark on the manga
	res, err := e.svc.Hold(ctx, HoldOptions{Watch: w.ID, Mark: storage.HoldHiatus})
	if err != nil {
		t.Fatal(err)
	}
	if res.Manga != manga || res.Check == nil || res.Check.Kind != KindHold || !slices.Equal(src.downloaded, []float32{3}) {
		t.Errorf("hold %+v, downloaded %v", res, src.downloaded)
	}
	if len(res.Watches) != 1 || res.Watches[0].Enabled || res.Watches[0].Hold != storage.HoldHiatus || res.Watches[0].Scheduled() {
		t.Errorf("held watches %+v", res.Watches)
	}
	if lm, _, _ := e.svc.Library.MangaByID(manga); lm.Hold != storage.HoldHiatus {
		t.Errorf("library manga %+v", lm)
	}
	if _, err := e.svc.SetEnabled(ctx, w.ID, true); !errors.Is(err, ErrHeld) {
		t.Errorf("resumed on hold: %v", err)
	}
	if ErrorCode(ErrHeld) != CodeHeld || !errors.Is(&APIError{Code: CodeHeld}, ErrHeld) {
		t.Error("held error code")
	}
	// never on schedule, but when asked (with gaps too)
	if due, _ := e.svc.Due(ctx, e.now.Add(48*time.Hour)); len(due) != 0 {
		t.Errorf("due %+v", due)
	}
	src.chs["hxh"] = m.chapters(1, 2, 3, 4)
	if report, _ := e.svc.Check(ctx, []int64{w.ID}, CheckOptions{Gaps: true}); !slices.Equal(numbers(report.Results[0].Chapters), []float32{4}) {
		t.Errorf("asked check %+v", report.Results[0])
	}

	// cleared from the library manga: resumed
	res, err = e.svc.Hold(ctx, HoldOptions{Manga: manga, Mark: ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Watches) != 1 || !res.Watches[0].Enabled || res.Watches[0].Hold != "" || !res.Watches[0].Resumed.Equal(e.now) {
		t.Errorf("cleared %+v", res.Watches)
	}

	// ended without a check
	src.downloaded = nil
	src.chs["hxh"] = m.chapters(1, 2, 3, 4, 5)
	if res, err = e.svc.Hold(ctx, HoldOptions{Manga: manga, Mark: storage.HoldEnded, NoCheck: true}); err != nil || res.Check != nil || len(src.downloaded) != 0 {
		t.Errorf("no check %+v, %v, downloaded %v", res, err, src.downloaded)
	}
	if got, _ := e.svc.Get(ctx, w.ID); got.Hold != storage.HoldEnded || got.Enabled {
		t.Errorf("ended %+v", got)
	}
	// a paused watch that wasn't held stays paused when cleared
	_, _ = e.svc.Hold(ctx, HoldOptions{Manga: manga})

	// another provider: a re-link of its own kind, kept by a check that finds the manga
	res, err = e.svc.Hold(ctx, HoldOptions{Watch: w.ID, Mark: MarkProvider, Reason: "has 60-63 only"})
	if err != nil {
		t.Fatal(err)
	}
	got := res.Watches[0]
	if !got.NeedsRelink || got.RelinkKind != storage.RelinkProvider || got.RelinkReason != "has 60-63 only" || got.RelinkLabel() != "needs a new provider" {
		t.Errorf("provider mark %+v", got)
	}
	_, _ = e.svc.Check(ctx, []int64{w.ID}, CheckOptions{})
	if got, _ := e.svc.Get(ctx, w.ID); !got.NeedsRelink || got.RelinkKind != storage.RelinkProvider {
		t.Errorf("after a check %+v", got)
	}
	if res, _ = e.svc.Hold(ctx, HoldOptions{Watch: w.ID}); res.Watches[0].NeedsRelink {
		t.Errorf("not cleared %+v", res.Watches[0])
	}
}

func TestHoldWithoutLibraryManga(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	m := &fakeManga{info: mangadata.MangaInfo{ID: "new", Title: "New"}}
	src := e.source("p", m)
	src.chs["new"] = nil
	w, err := e.svc.Add(ctx, AddOptions{Provider: "p", MangaID: "new"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.svc.Hold(ctx, HoldOptions{Watch: w.ID, Mark: storage.HoldEnded})
	if err != nil {
		t.Fatal(err)
	}
	if res.Manga != 0 || res.Watches[0].Hold != storage.HoldEnded || res.Watches[0].Watch.Hold != storage.HoldEnded {
		t.Errorf("own hold %+v", res)
	}
	// a library manga without watches gets the provider mark itself
	e.own(t, "q", &fakeManga{info: mangadata.MangaInfo{ID: "x", Title: "X"}}, 1)
	lm, _, _ := e.svc.Library.MangaBySource("q", "x")
	if res, err := e.svc.Hold(ctx, HoldOptions{Manga: lm.ID, Mark: MarkProvider}); err != nil || len(res.Watches) != 0 {
		t.Fatalf("%+v, %v", res, err)
	}
	if got, _, _ := e.svc.Library.MangaByID(lm.ID); got.NewProvider != "marked by hand" {
		t.Errorf("library mark %+v", got)
	}
}

func TestSuggestedHold(t *testing.T) {
	for status, want := range map[string]string{"HIATUS": storage.HoldHiatus, "FINISHED": storage.HoldEnded, "CANCELLED": storage.HoldEnded, "RELEASING": "", "": ""} {
		if got := SuggestedHold(status); got != want {
			t.Errorf("%q: %q, want %q", status, got, want)
		}
	}
}

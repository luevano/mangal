package jobkinds

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/util"
)

// fakeKinds are two kinds, what's asked of them recorded.
type fakeKinds struct {
	core.JobKinds
	list  []core.JobKind
	asked []string
	set   []core.JobSettings
}

func (f *fakeKinds) List(context.Context) ([]core.JobKind, error) { return f.list, nil }

func (f *fakeKinds) Get(_ context.Context, kind jobs.Kind) (core.JobKind, error) {
	for _, k := range f.list {
		if k.Kind == kind {
			return k, nil
		}
	}
	return core.JobKind{}, core.ErrNotFound
}

func (f *fakeKinds) Run(_ context.Context, kind jobs.Kind, all bool) (core.Job, error) {
	f.asked = append(f.asked, "run "+kind.String()+map[bool]string{true: " all", false: " missing"}[all])
	if kind == jobs.KindScan && !all {
		return core.Job{}, core.ErrNothingToDo
	}
	return core.Job{ID: 3, Kind: kind, Title: kind.String() + " job"}, nil
}

func (f *fakeKinds) Pause(_ context.Context, kind jobs.Kind, paused bool) (core.JobKind, error) {
	f.asked = append(f.asked, "pause "+kind.String())
	k, err := f.Get(context.Background(), kind)
	k.Paused = paused
	return k, err
}

func (f *fakeKinds) Set(_ context.Context, kind jobs.Kind, s core.JobSettings) (core.JobKind, error) {
	f.set = append(f.set, s)
	return f.Get(context.Background(), kind)
}

func withKinds(t *testing.T) *fakeKinds {
	t.Helper()
	missing := 2
	f := &fakeKinds{list: []core.JobKind{
		{Kind: jobs.KindSourceURLs, Title: "Source addresses", Enabled: true, Schedule: "168h", ScheduleKey: "jobs.urls.interval", ProvidersKey: "jobs.urls.providers",
			Missing: &missing, Next: time.Date(2026, 10, 27, 12, 0, 0, 0, time.Local), Last: &core.JobRun{Summary: "3 found, 0 gone, 0 failed", Finished: time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)},
			Providers: []core.JobProvider{{Provider: "mango-mangadex", Name: "MangaDex", On: true, Items: 3, Missing: 2}, {Provider: "mango-mangaplus", Name: "MangaPlus", Default: "off"}}},
		{Kind: jobs.KindScan, Title: "Library scan", Manual: true, NoNext: "manual"},
	}}
	old := Kinds
	Kinds = func() (core.JobKinds, error) { return f, nil }
	t.Cleanup(func() { Kinds = old })
	return f
}

// feed feeds the msgs to s until nothing comes back.
func feed(s base.State, msgs ...tea.Msg) {
	ctx := context.Background()
	for len(msgs) != 0 {
		msg := msgs[0]
		msgs = append(msgs[1:], base.Drain(s.Update(ctx, msg))...)
	}
}

func TestJobsScreen(t *testing.T) {
	f := withKinds(t)
	s := New().(*state)
	s.Resize(base.Size{Width: 160, Height: 30})
	feed(s, base.Drain(s.Init(context.Background()))...)
	view := ansi.Strip(s.View())
	for _, want := range []string{"Source addresses on", "3 found, 0 gone, 0 failed · next 2026-10-27 12:00 · 2 missing", "Library scan manual"} {
		if !strings.Contains(view, want) {
			t.Errorf("no %q in:\n%s", want, view)
		}
	}
	feed(s, util.KeyPress("C"), util.KeyPress("u"), util.KeyPress("p"))
	if got := strings.Join(f.asked, ", "); got != "run urls missing, run urls all, pause urls" {
		t.Errorf("asked %q", got)
	}
	if cmd := s.Update(context.Background(), util.KeyPress("enter")); cmd == nil {
		t.Error("enter opened nothing")
	} else if d, ok := cmd().(*detail); !ok || d.k.Kind != jobs.KindSourceURLs {
		t.Errorf("enter opened %#v", cmd())
	}
	if cmd := s.Update(context.Background(), util.KeyPress("e")); cmd == nil {
		t.Error("e opened nothing")
	} else if _, ok := cmd().(*settings); !ok {
		t.Errorf("e opened %#v", cmd())
	}
	// the scan has no missing ones, nor a schedule to pause
	s.Update(context.Background(), util.KeyPress("down"))
	if s.keys.runMissing.Enabled() || s.keys.pause.Enabled() || !s.keys.runAll.Enabled() {
		t.Errorf("scan keys: missing %t pause %t", s.keys.runMissing.Enabled(), s.keys.pause.Enabled())
	}
}

func TestDetail(t *testing.T) {
	f := withKinds(t)
	d := newDetail(f.list[0]).(*detail)
	d.Resize(base.Size{Width: 160, Height: 30})
	feed(d, base.Drain(d.Init(context.Background()))...)
	if view := ansi.Strip(d.View()); !strings.Contains(view, "every 168h (the kind's) · 3 items, 2 missing") || !strings.Contains(view, "off MangaPlus") {
		t.Errorf("view:\n%s", view)
	}
	feed(d, util.KeyPress("space"), util.KeyPress("down"), util.KeyPress("backspace"))
	if len(f.set) != 2 || f.set[0].Providers["mango-mangadex"] != "off" || f.set[1].Providers["mango-mangaplus"] != "" {
		t.Errorf("set %+v", f.set)
	}
}

func TestSettings(t *testing.T) {
	f := withKinds(t)
	s := newSettings(f.list[0]).(*settings)
	s.Init(context.Background())
	// the fields: on, schedule, pause, MangaDex, MangaPlus
	if len(s.providers) != 2 || s.providers[1].Value() != "off" || s.providers[0].Value() != "" {
		t.Fatalf("providers %q %q", s.providers[0].Value(), s.providers[1].Value())
	}
	s.providers[0].SetValue("336h")
	s.providers[1].SetValue("on")
	s.schedule.SetValue("72h")
	c := s.changes()
	if c.Schedule == nil || *c.Schedule != "72h" || c.Providers["mango-mangadex"] != "336h" || c.Providers["mango-mangaplus"] != "on" || c.Enabled != nil || c.DelayMin != nil {
		t.Errorf("changes %+v", c)
	}
	s.providers[0].SetValue("soon")
	if s.form.Valid() {
		t.Error("an interval that isn't one is valid")
	}
}

func TestKeys(t *testing.T) {
	f := withKinds(t)
	s := New().(*state)
	feed(s, base.Drain(s.Init(context.Background()))...)
	if keys := base.KeyCollisions(s.keyMap(), s.List.KeyMap.Browsing()...); len(keys) != 0 {
		t.Errorf("jobs: %q", keys)
	}
	d := newDetail(f.list[0]).(*detail)
	if keys := base.KeyCollisions(d.keyMap(), d.List.KeyMap.Browsing()...); len(keys) != 0 {
		t.Errorf("detail: %q", keys)
	}
	if bad := base.KeyInconsistencies(newSettings(f.list[0]).KeyMap()); len(bad) != 0 {
		t.Errorf("settings: %q", bad)
	}
}

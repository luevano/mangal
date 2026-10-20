package chapters

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/tui/base"
	tuijobs "github.com/luevano/mangal/v5/tui/jobs"
	"github.com/luevano/mangal/v5/tui/state/pick"
)

func TestFill(t *testing.T) {
	ctx := context.Background()
	c, err := libmangal.NewClient(fakeLoader{}, libmangal.DefaultClientOptions())
	if err != nil {
		t.Fatal(err)
	}
	get, save := fillProviders, saveFillProviders
	t.Cleanup(func() { fillProviders, saveFillProviders = get, save })
	fillProviders = func() []string { return nil }
	manga, list := chapters(2)
	if _, ok := New(c, manga, nil, list).(*state).Update(ctx, press("alt+g"))().(*pick.Providers); ok {
		t.Error("alt+g opens the picker without unavailable chapters")
	}
	list = append(list, fakeChapter{volume: fakeVolume{manga}, number: 3, unavailable: "retired"})
	if _, ok := New(c, manga, nil, list).(*state).Update(ctx, press("alt+g"))().(*pick.Providers); !ok {
		t.Fatal("alt+g doesn't open the provider picker")
	}

	s := newFillState("fake", manga, []libmangal.ProviderInfo{{ID: "mango-mangapill", Name: "Mangapill"}, {ID: "mango-weebcentral", Name: "Weeb Central"}}, "chapters/fake/berserk")
	s.Resize(base.Size{Width: 120, Height: 40})
	if s.Search.Query() != "Berserk" || s.Subtitle() != "on Mangapill, Weeb Central" {
		t.Errorf("search %q %s", s.Search.Query(), s.Subtitle())
	}
	if req := s.req("mango-mangapill", &fakeManga{id: "mp"}); req.Provider != "fake" || req.MangaID != "berserk" || req.From.Provider != "mango-mangapill" || req.From.MangaID != "mp" {
		t.Errorf("req %+v", req)
	}
	msg := fillPlannedMsg{core.FillPlan{Manga: manga.Info(), From: libmangal.ProviderInfo{ID: "mango-mangapill", Name: "Mangapill"},
		Found: []core.FillChapter{{Number: 3, Title: "Chapter 3", FromTitle: "Third", URL: "https://mangapill.com/3"}}}}
	s.Update(ctx, msg)
	if !s.Confirm.Active() || !strings.Contains(s.View(), `get chapter 3 "Chapter 3" from mango-mangapill`) {
		t.Fatalf("no confirmation:\n%s", s.View())
	}

	// a closed manager: the job is listed, it doesn't run
	m := jobs.New(ctx)
	m.Shutdown(0)
	tuijobs.SetManager(m)
	t.Cleanup(func() { tuijobs.SetManager(nil) })
	cmd := s.Update(ctx, press("y"))
	if msg := cmd(); msg != tea.Msg(base.Back()) {
		t.Errorf("yes: %#v, want back", msg)
	}
	if list := m.Jobs(); len(list) != 1 || list[0].Title != "Berserk: chapter 3 from Mangapill" || list[0].Origin != "chapters/fake/berserk" {
		t.Errorf("jobs %+v", list)
	}
}

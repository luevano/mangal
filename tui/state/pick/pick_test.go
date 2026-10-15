package pick

import (
	"github.com/luevano/mangal/v5/tui/util"

	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/tui/base"
)

type testManga struct{ info mangadata.MangaInfo }

func (m *testManga) String() string                { return m.info.Title }
func (m *testManga) Info() mangadata.MangaInfo     { return m.info }
func (m *testManga) Metadata() metadata.Metadata   { return nil }
func (m *testManga) SetMetadata(metadata.Metadata) {}

func manga(id string) mangadata.Manga {
	return &testManga{mangadata.MangaInfo{ID: id, Title: "Manga " + id, URL: "https://example.org/" + id}}
}

// fakes swaps the providers: searches give "<provider>-1" and "<provider>-2", the
// provider's current id is "<provider>-new".
func fakes(t *testing.T) *[]string {
	var searched []string
	search, installed := SearchProvider, InstalledProviders
	t.Cleanup(func() { SearchProvider, InstalledProviders = search, installed })
	SearchProvider = func(_ context.Context, provider, query string) ([]mangadata.Manga, string, error) {
		searched = append(searched, provider+": "+query)
		return []mangadata.Manga{manga(provider + "-1"), manga(provider + "-2")}, provider + "-new", nil
	}
	InstalledProviders = func() ([]libmangal.ProviderInfo, error) {
		return []libmangal.ProviderInfo{{ID: "a", Name: "Alpha"}, {ID: "b", Name: "Beta"}, {ID: "c", Name: "Gamma"}}, nil
	}
	return &searched
}

func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return util.KeyPress("enter")
	case "tab":
		return util.KeyPress("tab")
	case "down":
		return util.KeyPress("down")
	}
	return util.KeyPress(k)
}

// run gives the screen the msgs of cmd, and of what they start.
func run(s base.State, cmd tea.Cmd) []tea.Msg {
	var other []tea.Msg
	for _, msg := range base.Drain(cmd) {
		switch msg.(type) {
		case ResultsMsg, providersMsg, ProviderPickedMsg:
			other = append(other, run(s, s.Update(context.Background(), msg))...)
		default:
			other = append(other, msg)
		}
	}
	return other
}

func TestMangaSearch(t *testing.T) {
	searched := fakes(t)
	var picked []string
	s := NewManga(MangaOptions{
		Title: "Re-link", Provider: "a", Query: "frieren", Action: "re-link",
		Current: func(provider, current string, m mangadata.Manga) bool { return m.Info().ID == current[:1]+"-2" },
		Pick: func(_ context.Context, provider string, m mangadata.Manga) tea.Cmd {
			picked = append(picked, provider+"/"+m.Info().ID)
			return nil
		},
	})
	s.Resize(base.Size{Width: 80, Height: 20})
	run(s, s.Init(context.Background()))
	if len(*searched) != 1 || (*searched)[0] != "a: frieren" {
		t.Fatalf("searched %q", *searched)
	}
	// the provider's current id from the search
	if s.Provider() != "a-new" || !strings.Contains(s.Subtitle(), "on a-new") {
		t.Errorf("provider %q, subtitle %q", s.Provider(), s.Subtitle())
	}
	if view := s.View(); !strings.Contains(view, "Manga a-1") || !strings.Contains(view, "Manga a-2 (current)") {
		t.Errorf("view:\n%s", view)
	}
	run(s, s.Update(context.Background(), keyMsg("down")))
	run(s, s.Update(context.Background(), keyMsg("enter")))
	if len(picked) != 1 || picked[0] != "a-new/a-2" {
		t.Errorf("picked %q", picked)
	}

	// tab picks another provider, the search runs again there
	cmd := s.Update(context.Background(), keyMsg("tab"))
	if p, ok := cmd().(*Providers); !ok || p.current != "a-new" {
		t.Fatalf("tab: %#v", cmd())
	}
	run(s, s.Update(context.Background(), ProviderPickedMsg{Provider: "b"}))
	if len(*searched) != 2 || (*searched)[1] != "b: frieren" || !strings.Contains(s.View(), "Manga b-1") {
		t.Errorf("searched %q\n%s", *searched, s.View())
	}

	// s searches again
	run(s, s.Update(context.Background(), keyMsg("s")))
	if !s.Inputting() {
		t.Error("s doesn't focus the search")
	}
}

// a late result of an older search is dropped
func TestMangaLateResults(t *testing.T) {
	fakes(t)
	s := NewManga(MangaOptions{Provider: "a", Pick: func(context.Context, string, mangadata.Manga) tea.Cmd { return nil }})
	s.Resize(base.Size{Width: 80, Height: 20})
	old, _ := s.Results(context.Background(), "old", "q")
	old.Search = s.NewSearch()
	latest, _ := s.Results(context.Background(), "new", "q")
	latest.Search = s.NewSearch()
	s.Update(context.Background(), latest)
	s.Update(context.Background(), old)
	if v := s.View(); strings.Contains(v, "old-1") || !strings.Contains(v, "new-1") {
		t.Errorf("view:\n%s", v)
	}
}

func TestProviders(t *testing.T) {
	fakes(t)
	var picked string
	p := NewProviders("b", func(provider string) tea.Cmd {
		picked = provider
		return nil
	})
	p.Resize(base.Size{Width: 80, Height: 20})
	run(p, p.Init(context.Background()))
	if !strings.Contains(p.View(), "Beta (current)") {
		t.Errorf("view:\n%s", p.View())
	}
	// the current one is selected first
	run(p, p.Update(context.Background(), keyMsg("enter")))
	if picked != "b" {
		t.Errorf("picked %q", picked)
	}

	// without a pick it goes back with the provider
	p = NewProviders("", nil)
	run(p, p.Init(context.Background()))
	msgs := base.Drain(p.Update(context.Background(), keyMsg("enter")))
	if len(msgs) != 2 || msgs[1] != (ProviderPickedMsg{Provider: "a"}) {
		t.Errorf("msgs %#v", msgs)
	}
	if _, ok := msgs[0].(base.BackMsg); !ok {
		t.Errorf("%T, want back", msgs[0])
	}
}

func TestKeys(t *testing.T) {
	m := NewManga(MangaOptions{Action: "re-link"})
	p := NewProviders("", nil)
	for name, keys := range map[string][]string{
		"manga":     base.KeyCollisions(m.Keys(), m.List.KeyMap.Browsing()...),
		"providers": base.KeyCollisions(p.Keys(), p.List.KeyMap.Browsing()...),
	} {
		if len(keys) != 0 {
			t.Errorf("%s: %v", name, keys)
		}
	}
}

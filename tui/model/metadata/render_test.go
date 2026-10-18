package metadata

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/luevano/libmangal/metadata"
)

type fakeMeta struct{}

func (fakeMeta) String() string { return "Frieren (2020)" }
func (fakeMeta) Title() string  { return "Frieren: Beyond Journey's End" }
func (fakeMeta) AlternateTitles() []string {
	return []string{"Sousou no Frieren", "葬送のフリーレン", "Frieren at the Funeral"}
}
func (fakeMeta) Score() float32 { return 4.6 }
func (fakeMeta) Description() string {
	return strings.Repeat("The adventure is over but life goes on for an elf mage just beginning to learn what living is all about. ", 4)
}
func (fakeMeta) Cover() string {
	return "https://s4.anilist.co/file/anilistcdn/media/manga/cover/large/bx118586-F0Lp86XQV7du.jpg"
}
func (fakeMeta) Banner() string {
	return "https://s4.anilist.co/file/anilistcdn/media/manga/banner/118586-1JLJiwaIlnWP.jpg"
}
func (fakeMeta) Tags() []string        { return []string{"Elf", "Magic", "Travel", "Time Skip"} }
func (fakeMeta) Genres() []string      { return []string{"Adventure", "Drama", "Fantasy"} }
func (fakeMeta) Characters() []string  { return []string{"Frieren", "Fern", "Stark", "Himmel"} }
func (fakeMeta) Authors() []string     { return []string{"Kanehito Yamada"} }
func (fakeMeta) Artists() []string     { return []string{"Tsukasa Abe"} }
func (fakeMeta) Translators() []string { return nil }
func (fakeMeta) Letterers() []string   { return nil }
func (fakeMeta) StartDate() metadata.Date {
	return metadata.Date{Year: 2020, Month: 4, Day: 28}
}
func (fakeMeta) EndDate() metadata.Date  { return metadata.Date{} }
func (fakeMeta) Publisher() string       { return "Shogakukan" }
func (fakeMeta) Status() metadata.Status { return metadata.StatusReleasing }
func (fakeMeta) Format() string          { return "MANGA" }
func (fakeMeta) Country() string         { return "JP" }
func (fakeMeta) Chapters() int           { return 140 }
func (fakeMeta) Notes() string           { return "" }
func (fakeMeta) URL() string             { return "https://anilist.co/manga/118586/Sousou-no-Frieren/" }
func (fakeMeta) ID() metadata.ID {
	return metadata.ID{Raw: "118586", Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}
}
func (fakeMeta) ExtraIDs() []metadata.ID {
	return []metadata.ID{{Raw: "126287", Source: metadata.IDSourceMyAnimeList, Code: metadata.IDCodeMyAnimeList}}
}

// TestDetailsWidth: the details fit the width, and nothing is cut.
func TestDetailsWidth(t *testing.T) {
	words := func(s string) string { return strings.Join(strings.Fields(ansi.Strip(s)), "") }
	for _, width := range []int{60, 120} {
		details := Details(fakeMeta{}, width)
		for line := range strings.SplitSeq(details, "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("%d columns: a line of %d: %q", width, w, ansi.Strip(line))
			}
		}
		all := words(details)
		for _, want := range []string{fakeMeta{}.Description(), fakeMeta{}.Cover(), fakeMeta{}.URL(), "SousounoFrieren", "KanehitoYamada"} {
			if !strings.Contains(all, words(want)) {
				t.Errorf("%d columns: %.40q... cut", width, want)
			}
		}
	}
	// wide enough: as without a limit
	if w := lipgloss.Width(Details(fakeMeta{}, 0)); w <= 60 || w > 120 {
		t.Errorf("no limit: %d columns", w)
	}
	if Details(fakeMeta{}, 120) != Details(fakeMeta{}, 0) {
		t.Error("120 columns isn't the layout without a limit")
	}
}

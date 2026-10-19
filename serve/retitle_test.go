package serve

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/web/apiv1"
	"github.com/spf13/afero"
)

// TestV1Retitle: the plan has the source's titles, the apply records them.
func TestV1Retitle(t *testing.T) {
	s, _, _ := newHealthServer(t)
	newCore := s.api.Core
	var asked []string
	s.api.Core = func(m watch.Manager) (*core.Core, error) {
		c, err := newCore(m)
		if err != nil {
			return nil, err
		}
		env := c.Env()
		env.AllChapters = func(_ context.Context, provider, id, query, _ string) (libmangal.ProviderInfo, mangadata.Manga, []mangadata.Chapter, error) {
			asked = append(asked, provider+" "+id+" "+query)
			manga := &fakeManga{info: mangadata.MangaInfo{ID: "opm", Title: "One-Punch Man"}}
			vol := &fakeVolume{manga: manga}
			var chapters []mangadata.Chapter
			for _, info := range []mangadata.ChapterInfo{{Number: 1}, {Number: 1, Title: "One Punch"}, {Number: 2, Title: "Two", Unavailable: "retired"}} {
				chapters = append(chapters, &fakeChapter{info: info, vol: vol})
			}
			return libmangal.ProviderInfo{ID: provider}, manga, chapters, nil
		}
		return core.New(context.Background(), env)
	}
	c, err := s.api.services(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{1, 2, 4} {
		if err := afero.WriteFile(c.Env().FS, fmt.Sprintf("/lib/One-Punch Man/%d.cbz", n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var plan apiv1.ChangeResponse
	if code := s.call(t, "POST", "/api/v1/library/mangas/1/retitle", `{}`, &plan); code != http.StatusOK || plan.Plan == nil || plan.Plan.Kind != core.KindRetitle {
		t.Fatalf("plan %d %+v", code, plan)
	}
	lines := strings.Join(plan.Plan.Lines, "\n")
	if !strings.Contains(lines, `"One Punch"`) || !strings.Contains(lines, `"Two"`) || !strings.Contains(lines, "no title for chapters 4") {
		t.Fatalf("plan lines:\n%s", lines)
	}
	if len(asked) != 1 || asked[0] != "fake opm One-Punch Man" {
		t.Errorf("default source: %q", asked)
	}
	var applied apiv1.ChangeResponse
	body := fmt.Sprintf(`{"expect":%q}`, plan.Plan.Hash)
	if code := s.call(t, "POST", "/api/v1/library/mangas/1/retitle?apply=1", body, &applied); code != http.StatusOK || !applied.Applied || applied.Result.OpID == 0 {
		t.Fatalf("apply %d %+v", code, applied)
	}
	dls, err := s.src.library.Downloads(1)
	if err != nil {
		t.Fatal(err)
	}
	if dls[0].ChapterTitle != "One Punch" || dls[1].ChapterTitle != "Two" || dls[2].ChapterTitle != "" {
		t.Errorf("downloads %+v", dls)
	}

	var e ErrorResponse
	if code := s.call(t, "POST", "/api/v1/library/mangas/99/retitle", `{}`, &e); code != http.StatusNotFound {
		t.Errorf("missing manga: %d %+v", code, e)
	}
	if code := s.call(t, "POST", "/api/v1/library/mangas/1/retitle", `{"provider":"nope"}`, &e); code != http.StatusNotFound {
		t.Errorf("missing provider: %d %+v", code, e)
	}
}

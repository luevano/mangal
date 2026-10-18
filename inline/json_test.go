package inline

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/logger"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
)

type fakeManga struct {
	MangaInfo mangadata.MangaInfo `json:"info"`
}

func (m *fakeManga) String() string                { return m.MangaInfo.Title }
func (m *fakeManga) Info() mangadata.MangaInfo     { return m.MangaInfo }
func (m *fakeManga) Metadata() metadata.Metadata   { return nil }
func (m *fakeManga) SetMetadata(metadata.Metadata) {}

type fakeVolume struct{ manga *fakeManga }

func (v fakeVolume) String() string             { return "Vol. 1" }
func (v fakeVolume) Info() mangadata.VolumeInfo { return mangadata.VolumeInfo{Number: 1} }
func (v fakeVolume) Manga() mangadata.Manga     { return v.manga }

type fakeChapter struct {
	ChapterInfo mangadata.ChapterInfo `json:"info"`
	volume      fakeVolume
}

func (c fakeChapter) String() string              { return c.ChapterInfo.Title }
func (c fakeChapter) Info() mangadata.ChapterInfo { return c.ChapterInfo }
func (c fakeChapter) Volume() mangadata.Volume    { return c.volume }

type fakeLoader struct{}

func (fakeLoader) String() string { return "fake" }
func (fakeLoader) Info() libmangal.ProviderInfo {
	return libmangal.ProviderInfo{ID: "fake", Name: "Fake", Version: "0.1.0"}
}
func (l fakeLoader) Load(context.Context) (libmangal.Provider, error) { return &fakeProvider{}, nil }

type fakeProvider struct{ libmangal.Provider }

func (*fakeProvider) Info() libmangal.ProviderInfo { return fakeLoader{}.Info() }
func (*fakeProvider) String() string               { return "Fake" }
func (*fakeProvider) SetLogger(*logger.Logger)     {}
func (*fakeProvider) Close() error                 { return nil }
func (*fakeProvider) SearchMangas(_ context.Context, query string) ([]mangadata.Manga, error) {
	return []mangadata.Manga{
		&fakeManga{mangadata.MangaInfo{Title: "Frieren", ID: "frieren", URL: "https://example.org/frieren"}},
		&fakeManga{mangadata.MangaInfo{Title: "Frieren: Side Stories", ID: "side"}},
	}, nil
}
func (*fakeProvider) MangaVolumes(_ context.Context, m mangadata.Manga) ([]mangadata.Volume, error) {
	return []mangadata.Volume{fakeVolume{m.(*fakeManga)}}, nil
}
func (*fakeProvider) VolumeChapters(_ context.Context, v mangadata.Volume) ([]mangadata.Chapter, error) {
	var chapters []mangadata.Chapter
	for _, n := range []float32{1, 2, 3} {
		chapters = append(chapters, fakeChapter{mangadata.ChapterInfo{Title: "Chapter", Number: n}, v.(fakeVolume)})
	}
	return chapters, nil
}

// stdout is what f prints.
func stdout(t *testing.T, f func()) []byte {
	t.Helper()
	return capture(t, &os.Stdout, f)
}

// capture is what f writes to file (os.Stdout, os.Stderr).
func capture(t *testing.T, file **os.File, f func()) []byte {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := *file
	*file = w
	done := make(chan []byte)
	go func() {
		b, _ := io.ReadAll(r)
		done <- b
	}()
	f()
	*file = old
	w.Close()
	return <-done
}

// TestJSONShape: what scripts read from mangal inline --json.
func TestJSONShape(t *testing.T) {
	ctx := context.Background()
	c, err := libmangal.NewClientContext(ctx, fakeLoader{}, libmangal.DefaultClientOptions())
	if err != nil {
		t.Fatal(err)
	}
	old := acquire
	t.Cleanup(func() { acquire = old })
	acquire = func(context.Context, string) (*libmangal.Client, []string, error) { return c, nil, nil }

	args := Args{Query: "frieren", Provider: "fake", MangaSelector: "first", ChapterSelector: "last", ChapterPopulate: true, AnilistDisable: true, JSONOutput: true}
	var runErr error
	out := stdout(t, func() { runErr = RunJSON(ctx, args) })
	if runErr != nil {
		t.Fatal(runErr)
	}

	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	if keys := mapKeys(got); !slices.Equal(keys, []string{"query_params", "results"}) {
		t.Errorf("top keys %q", keys)
	}
	params := got["query_params"].(map[string]any)
	if keys := mapKeys(params); !slices.Equal(keys, []string{"anilist_disable", "anilist_id", "chapter_populate", "chapter_selector", "json_output", "manga_selector", "prefer_provider_metadata", "provider", "query"}) {
		t.Errorf("query_params keys %q", keys)
	}
	results := got["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("%d results", len(results))
	}
	result := results[0].(map[string]any)
	if keys := mapKeys(result); !slices.Equal(keys, []string{"anilist", "chapters", "index", "manga", "metadata"}) {
		t.Errorf("result keys %q", keys)
	}
	if result["index"] != 0.0 || result["anilist"] != nil {
		t.Errorf("index %v, anilist %v", result["index"], result["anilist"])
	}
	manga := result["manga"].(map[string]any)["info"].(map[string]any)
	if manga["id"] != "frieren" || manga["title"] != "Frieren" {
		t.Errorf("manga %v", manga)
	}
	chapters := result["chapters"].([]any)
	if len(chapters) != 1 || !reflect.DeepEqual(chapters[0].(map[string]any)["info"].(map[string]any)["number"], 3.0) {
		t.Errorf("chapters %v", chapters)
	}
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// shortProvider: the side stories have only chapter 1.
type shortProvider struct{ fakeProvider }

func (p *shortProvider) VolumeChapters(ctx context.Context, v mangadata.Volume) ([]mangadata.Chapter, error) {
	chapters, err := p.fakeProvider.VolumeChapters(ctx, v)
	if v.Manga().Info().ID == "side" {
		chapters = chapters[:1]
	}
	return chapters, err
}

type shortLoader struct{ fakeLoader }

func (shortLoader) Load(context.Context) (libmangal.Provider, error) { return &shortProvider{}, nil }

// TestJSONChapterPopulateMiss: a manga the chapter selector picks nothing of gets no
// chapters when there are others, it fails the run when it's the only one.
func TestJSONChapterPopulateMiss(t *testing.T) {
	useClient(t, shortLoader{})
	args := Args{Query: "frieren", Provider: "fake", MangaSelector: "all", ChapterSelector: "2-3", ChapterPopulate: true, AnilistDisable: true}
	var runErr error
	out := stdout(t, func() { runErr = RunJSON(context.Background(), args) })
	if runErr != nil {
		t.Fatal(runErr)
	}
	var got struct {
		Results []struct {
			Chapters []any `json:"chapters"`
		} `json:"results"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	if len(got.Results) != 2 || len(got.Results[0].Chapters) != 2 || got.Results[1].Chapters == nil || len(got.Results[1].Chapters) != 0 {
		t.Errorf("results %+v", got.Results)
	}

	args.MangaSelector = "1"
	if err := RunJSON(context.Background(), args); err == nil {
		t.Error("the only manga: no error")
	}
}

// brokenProvider: the side stories' volumes fail.
type brokenProvider struct{ fakeProvider }

func (p *brokenProvider) MangaVolumes(ctx context.Context, m mangadata.Manga) ([]mangadata.Volume, error) {
	if m.Info().ID == "side" {
		return nil, errors.New("site down")
	}
	return p.fakeProvider.MangaVolumes(ctx, m)
}

type brokenLoader struct{ fakeLoader }

func (brokenLoader) Load(context.Context) (libmangal.Provider, error) { return &brokenProvider{}, nil }

// TestJSONChapterPopulateError: a manga whose chapters fail gets a warning and no chapters
// when there are others, it fails the run when it's the only one.
func TestJSONChapterPopulateError(t *testing.T) {
	useClient(t, brokenLoader{})
	args := Args{Query: "frieren", Provider: "fake", MangaSelector: "all", ChapterSelector: "all", ChapterPopulate: true, AnilistDisable: true}
	var runErr error
	var out []byte
	warning := capture(t, &os.Stderr, func() { out = stdout(t, func() { runErr = RunJSON(context.Background(), args) }) })
	if runErr != nil {
		t.Fatal(runErr)
	}
	if !strings.Contains(string(warning), "Frieren: Side Stories") || !strings.Contains(string(warning), "site down") {
		t.Errorf("warning %q", warning)
	}
	var got struct {
		Results []struct {
			Chapters []any `json:"chapters"`
		} `json:"results"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	if len(got.Results) != 2 || len(got.Results[0].Chapters) != 3 || got.Results[1].Chapters == nil || len(got.Results[1].Chapters) != 0 {
		t.Errorf("results %+v", got.Results)
	}

	args.MangaSelector = "1"
	if err := RunJSON(context.Background(), args); err == nil || !strings.Contains(err.Error(), "site down") {
		t.Errorf("the only manga: error %v", err)
	}
}

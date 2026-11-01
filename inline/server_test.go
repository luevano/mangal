package inline

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/luevano/mangal/v5/core/remote"
)

const providersJSON = `[{"id":"p","name":"P","version":"1"}]`

// fakeServer answers "METHOD /path" with the JSON, what it got is in asked.
func fakeServer(t *testing.T, answers map[string]string) (*remote.Client, *[]string) {
	t.Helper()
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		key := r.Method + " " + strings.TrimPrefix(r.URL.Path, "/api/v1")
		asked = append(asked, strings.TrimSpace(key+" "+string(body)))
		w.Header().Set("Content-Type", "application/json")
		answer, ok := answers[key]
		if !ok {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"not for you","code":"forbidden"}`))
			return
		}
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusAccepted)
		}
		_, _ = w.Write([]byte(answer))
	}))
	t.Cleanup(srv.Close)
	rc, err := remote.New(remote.Options{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return rc, &asked
}

// a server's download is its job, the result's paths are printed; a failed chapter is the exit code
func TestServerDownload(t *testing.T) {
	const done = `{"id":4,"kind":"download","title":"M: chapter 1","status":"done","providers":["p"],"added":"2026-11-01T00:00:00Z",
"results":[{"chapter":1,"title":"One","path":"/srv/One.cbz","status":"new"},{"chapter":2,"title":"Two","error":"gone"}]}`
	rc, asked := fakeServer(t, map[string]string{"POST /downloads": done, "GET /status": `{"api_version":"1.1.0"}`})
	var err error
	out := stdout(t, func() {
		err = RunDownload(t.Context(), Args{Server: rc, Provider: "p", Query: "M", MangaSelector: "first", ChapterSelector: "last"})
	})
	if strings.TrimSpace(string(out)) != "/srv/One.cbz" {
		t.Errorf("out %q", out)
	}
	if err == nil || !strings.Contains(err.Error(), "1 of 2 chapters failed") {
		t.Errorf("err %v", err)
	}
	if got := (*asked)[0]; !strings.Contains(got, `"selector":"first"`) || !strings.Contains(got, `"chapters":"last"`) || !strings.Contains(got, `"query":"M"`) {
		t.Errorf("asked %s", got)
	}
	if err := RunDownload(t.Context(), Args{Server: rc, Provider: "p", Query: "M", AnilistID: 3}); err == nil || !strings.Contains(err.Error(), "--local") {
		t.Errorf("--anilist-id: %v", err)
	}
}

// a server's providers and metadata: the JSON says what the API has
func TestServerJSON(t *testing.T) {
	rc, asked := fakeServer(t, map[string]string{
		"GET /providers":               providersJSON,
		"GET /providers/p/search":      `[{"id":"m1","title":"Frieren","url":"https://x/m1"}]`,
		"GET /bindings":                `[]`,
		"GET /metadata/sources":        `[{"key":"anilist","code":"al","name":"AniList","enabled":true}]`,
		"GET /metadata/anilist/search": `[{"id":"30013","title":"Frieren","url":"https://a/30013","score":0.9,"bad":false}]`,
	})
	var err error
	out := stdout(t, func() {
		err = RunJSON(t.Context(), Args{Server: rc, Provider: "p", Query: "Frieren", MangaSelector: "all", ChapterSelector: "all"})
	})
	if err != nil {
		t.Fatalf("%v\n%v", err, *asked)
	}
	var got struct {
		Results []struct {
			Manga          map[string]any `json:"manga"`
			Metadata       map[string]any `json:"metadata"`
			MetadataSource string         `json:"metadata_source"`
		} `json:"results"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if len(got.Results) != 1 || len(got.Results[0].Manga) == 0 || got.Results[0].MetadataSource != "anilist" || got.Results[0].Metadata["title"] != "Frieren" {
		t.Errorf("%s", out)
	}
}

func TestServerProviderMissing(t *testing.T) {
	rc, _ := fakeServer(t, map[string]string{"GET /providers": providersJSON})
	err := RunJSON(t.Context(), Args{Server: rc, Provider: "gone", Query: "x", MangaSelector: "all", ChapterSelector: "all"})
	if err == nil || !strings.Contains(err.Error(), `"gone"`) || !strings.Contains(err.Error(), "--local") {
		t.Errorf("%v", err)
	}
}

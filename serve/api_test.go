package serve

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/web"
)

type fakeManga struct {
	info mangadata.MangaInfo
	meta metadata.Metadata
}

func (m *fakeManga) String() string                     { return m.info.Title }
func (m *fakeManga) Info() mangadata.MangaInfo          { return m.info }
func (m *fakeManga) Metadata() metadata.Metadata        { return m.meta }
func (m *fakeManga) SetMetadata(meta metadata.Metadata) { m.meta = meta }

type fakeVolume struct{ manga *fakeManga }

func (v *fakeVolume) String() string             { return "vol" }
func (v *fakeVolume) Info() mangadata.VolumeInfo { return mangadata.VolumeInfo{Number: 1} }
func (v *fakeVolume) Manga() mangadata.Manga     { return v.manga }

type fakeChapter struct {
	info mangadata.ChapterInfo
	vol  *fakeVolume
}

func (c *fakeChapter) String() string              { return c.info.Title }
func (c *fakeChapter) Info() mangadata.ChapterInfo { return c.info }
func (c *fakeChapter) Volume() mangadata.Volume    { return c.vol }

// fakeSource has mangas with chapters 1..n, its downloads are recorded in the library.
type fakeSource struct {
	library storage.Library
	mangas  map[string]*fakeManga
	n       map[string]int

	mu sync.Mutex
	// block makes downloads wait for it (or the ctx), started is closed on the first one
	block   chan struct{}
	started chan struct{}
}

func (s *fakeSource) Info() libmangal.ProviderInfo {
	return libmangal.ProviderInfo{ID: "fake", Name: "fake"}
}

func (s *fakeSource) MangaByID(_ context.Context, id string) (mangadata.Manga, bool, error) {
	m, ok := s.mangas[id]
	return m, ok, nil
}

func (s *fakeSource) SearchMangas(_ context.Context, query string) ([]mangadata.Manga, error) {
	var found []mangadata.Manga
	for _, m := range s.mangas {
		if m.info.Title == query {
			found = append(found, m)
		}
	}
	return found, nil
}

func (s *fakeSource) Chapters(_ context.Context, manga mangadata.Manga) ([]mangadata.Chapter, error) {
	m := manga.(*fakeManga)
	var chs []mangadata.Chapter
	for i := 1; i <= s.n[m.info.ID]; i++ {
		chs = append(chs, &fakeChapter{info: mangadata.ChapterInfo{Number: float32(i), Title: fmt.Sprint(i)}, vol: &fakeVolume{m}})
	}
	return chs, nil
}

func (s *fakeSource) BoundMetadata(context.Context, metadata.IDCode, string) (metadata.Metadata, error) {
	return nil, nil
}

func (s *fakeSource) Download(ctx context.Context, chapters []mangadata.Chapter, _ func(client.Event)) client.Results {
	s.mu.Lock()
	block, started := s.block, s.started
	s.started = nil
	s.mu.Unlock()
	if started != nil {
		close(started)
	}
	results := client.NewResults(chapters)
	for _, r := range results {
		if block != nil {
			select {
			case <-block:
			case <-ctx.Done():
				r.Err = ctx.Err()
				continue
			}
		}
		info := r.Chapter.Info()
		manga := r.Chapter.Volume().Manga().Info()
		down := &metadata.DownloadedChapter{Number: info.Number, Directory: "/lib/" + manga.Title, Filename: fmt.Sprintf("%g.cbz", info.Number), Pages: 1}
		if err := s.library.RecordDownload("/lib", manga.Title, storage.Download{Path: down.Path(), Provider: "fake", MangaID: manga.ID,
			MangaTitle: manga.Title, Chapter: info.Number, Format: "CBZ", Downloaded: time.Now()}); err != nil {
			r.Err = err
			continue
		}
		r.Down = down
	}
	return results
}

func newTestService(t *testing.T) (*watch.Service, *fakeSource) {
	t.Helper()
	data, err := storage.OpenData(filepath.Join(t.TempDir(), storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	src := &fakeSource{library: storage.Library{DB: data},
		mangas: map[string]*fakeManga{
			"opm": {info: mangadata.MangaInfo{ID: "opm", Title: "One-Punch Man"}},
			"dan": {info: mangadata.MangaInfo{ID: "dan", Title: "Dandadan"}},
		},
		n: map[string]int{"opm": 3, "dan": 2},
	}
	svc := &watch.Service{
		Watches: storage.Watches{DB: data},
		Library: storage.Library{DB: data},
		Options: watch.Options{ParallelProviders: 2, Notify: watch.NotifyNone, LockStale: time.Minute},
		Source: func(_ context.Context, id string) (watch.Source, error) {
			if id != "fake" {
				return nil, fmt.Errorf("provider with id %q not found", id)
			}
			return src, nil
		},
		Now:     time.Now,
		OnError: func(err error) { t.Errorf("unexpected: %s", err) },
		Events:  watch.NewHub(),
	}
	return svc, src
}

type testServer struct {
	*httptest.Server
	api *API
	svc *watch.Service
	src *fakeSource
}

func newTestServer(t *testing.T, token string) *testServer {
	t.Helper()
	svc, src := newTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	api := &API{
		Manager: func() watch.Manager { return svc },
		Info:    watch.Status{Server: true, Version: "test", Started: time.Now()},
		Token:   token,
		Log:     newLogger(io.Discard),
		Runs:    &runs{},
		ctx:     ctx,
	}
	mux := http.NewServeMux()
	api.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		cancel()
		api.Wait()
		srv.Close()
	})
	return &testServer{Server: srv, api: api, svc: svc, src: src}
}

// call sends the request and decodes the answer into out (if not nil), returning the status.
func (s *testServer) call(t *testing.T, method, path string, body string, out any) int {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, s.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if out != nil && len(b) > 0 {
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatalf("%s %s: %s: %s", method, path, err, b)
		}
	}
	return resp.StatusCode
}

func TestAPIWatches(t *testing.T) {
	s := newTestServer(t, "")
	var w watch.Watch
	if code := s.call(t, "POST", "/api/watches", `{"provider":"fake","query":"mid:opm","from":1,"schedule":"0 */2 * * *"}`, &w); code != http.StatusCreated ||
		w.ID == 0 || w.Title != "One-Punch Man" || w.Schedule != "0 */2 * * *" || w.LastDownloaded == nil || *w.LastDownloaded != 1 {
		t.Fatalf("add: %d %+v", code, w)
	}
	var e watch.ErrorResponse
	if code := s.call(t, "POST", "/api/watches", `{"provider":"fake","manga_id":"opm"}`, &e); code != http.StatusConflict || e.Code != watch.CodeExists {
		t.Errorf("add twice: %d %+v", code, e)
	}
	if code := s.call(t, "POST", "/api/watches", `{"provider":"nope","manga_id":"opm"}`, &e); code != http.StatusUnprocessableEntity {
		t.Errorf("unknown provider: %d %+v", code, e)
	}
	if code := s.call(t, "POST", "/api/watches", `{"provider":"fake","bogus":1}`, &e); code != http.StatusBadRequest {
		t.Errorf("bad body: %d %+v", code, e)
	}
	var dan watch.Watch
	if code := s.call(t, "POST", "/api/watches", `{"provider":"fake","manga_id":"dan","anilist_id":132029}`, &dan); code != http.StatusCreated ||
		dan.AnilistID != 132029 || len(dan.Bindings) != 1 || dan.Bindings[0].ID != "132029" {
		t.Errorf("add with anilist_id: %d %+v", code, dan)
	}
	var list []watch.Watch
	if code := s.call(t, "GET", "/api/watches", "", &list); code != http.StatusOK || len(list) != 2 || list[1].AnilistID != 132029 {
		t.Errorf("list: %d %+v", code, list)
	}
	path := fmt.Sprintf("/api/watches/%d", w.ID)
	if code := s.call(t, "GET", path, "", &w); code != http.StatusOK || w.Provider != "fake" {
		t.Errorf("get: %d %+v", code, w)
	}
	if code := s.call(t, "GET", "/api/watches/999", "", &e); code != http.StatusNotFound || e.Code != watch.CodeNotFound {
		t.Errorf("get missing: %d %+v", code, e)
	}
	if code := s.call(t, "PATCH", path, `{"enabled":false,"gaps":true}`, &w); code != http.StatusOK || w.Enabled || !w.Gaps {
		t.Errorf("patch: %d %+v", code, w)
	}
	if code := s.call(t, "PATCH", path, `{"schedule":"every day"}`, &e); code != http.StatusBadRequest {
		t.Errorf("patch with an invalid schedule: %d %+v", code, e)
	}
	if code := s.call(t, "POST", "/api/watches", `{"provider":"fake","manga_id":"dan","schedule":"* * *"}`, &e); code != http.StatusBadRequest {
		t.Errorf("add with an invalid schedule: %d %+v", code, e)
	}
	if code := s.call(t, "PATCH", path, `{}`, &e); code != http.StatusBadRequest {
		t.Errorf("empty patch: %d", code)
	}
	// anilist_id is a shortcut for the default AniList binding
	if code := s.call(t, "PATCH", path, `{"anilist_id":21087}`, &w); code != http.StatusOK || w.AnilistID != 21087 ||
		len(w.Bindings) != 1 || w.Bindings[0].Source != "al" || w.Bindings[0].ID != "21087" || !w.Bindings[0].Default {
		t.Errorf("patch anilist_id: %d %+v", code, w)
	}
	var unbound watch.Watch
	if code := s.call(t, "PATCH", path, `{"anilist_id":0}`, &unbound); code != http.StatusOK || unbound.AnilistID != 0 || len(unbound.Bindings) != 0 {
		t.Errorf("patch anilist_id 0: %d %+v", code, unbound)
	}
	if code := s.call(t, "POST", path+"/relink", `{"manga_id":"dan"}`, &e); code != http.StatusConflict || e.Code != watch.CodeExists {
		t.Errorf("re-link to a watched manga: %d %+v", code, e)
	}
	if code := s.call(t, "POST", path+"/relink", `{}`, &e); code != http.StatusBadRequest {
		t.Errorf("empty re-link: %d %+v", code, e)
	}
	if code := s.call(t, "POST", path+"/relink", `{"provider":"fake","manga_id":"nope"}`, &e); code != http.StatusUnprocessableEntity {
		t.Errorf("re-link to a missing manga: %d %+v", code, e)
	}
	var stale []watch.Watch
	if code := s.call(t, "GET", "/api/watches/stale?since=0d", "", &stale); code != http.StatusOK || stale == nil {
		t.Errorf("stale: %d %+v", code, stale)
	}
	if code := s.call(t, "DELETE", path, "", nil); code != http.StatusNoContent {
		t.Errorf("delete: %d", code)
	}
	if code := s.call(t, "DELETE", path, "", &e); code != http.StatusNotFound {
		t.Errorf("delete twice: %d", code)
	}
}

func waitRun(t *testing.T, s *testServer, id int64) watch.CheckRun {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var run watch.CheckRun
		if code := s.call(t, "GET", fmt.Sprintf("/api/runs/%d", id), "", &run); code != http.StatusOK {
			t.Fatalf("run %d: %d", id, code)
		}
		if !run.Finished.IsZero() {
			return run
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %d didn't finish", id)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAPICheck(t *testing.T) {
	s := newTestServer(t, "")
	var w watch.Watch
	if code := s.call(t, "POST", "/api/watches", `{"provider":"fake","manga_id":"opm","from":1}`, &w); code != http.StatusCreated {
		t.Fatal(code)
	}
	block := make(chan struct{})
	started := make(chan struct{})
	s.src.mu.Lock()
	s.src.block, s.src.started = block, started
	s.src.mu.Unlock()

	var resp watch.CheckResponse
	if code := s.call(t, "POST", "/api/check", `{}`, &resp); code != http.StatusAccepted || resp.Run == 0 {
		t.Fatalf("check: %d %+v", code, resp)
	}
	<-started
	var st watch.Status
	if code := s.call(t, "GET", "/api/status", "", &st); code != http.StatusOK || !st.Server || !st.Checking || st.Run != resp.Run || st.Version != "test" ||
		len(st.Providers) != 1 || st.Providers[0].Provider != "fake" || st.Providers[0].Watch != w.ID || st.Providers[0].Run != resp.Run {
		t.Errorf("status: %d %+v", code, st)
	}
	var e watch.ErrorResponse
	if code := s.call(t, "POST", "/api/check", `{"ids":[1]}`, &e); code != http.StatusConflict || e.Code != watch.CodeBusy {
		t.Errorf("second check: %d %+v", code, e)
	}
	var dry watch.Report
	if code := s.call(t, "POST", "/api/check", `{"dry_run":true}`, &dry); code != http.StatusOK || !dry.DryRun || len(dry.Results) != 1 || len(dry.Results[0].Chapters) != 2 {
		t.Errorf("dry run while busy: %d %+v", code, dry)
	}
	if code := s.call(t, "POST", "/api/check", `{"ids":[999]}`, &e); code != http.StatusNotFound {
		t.Errorf("missing watch: %d %+v", code, e)
	}
	close(block)
	run := waitRun(t, s, resp.Run)
	if run.Kind != watch.KindManual || run.NewChapters != 2 || len(run.Results) != 1 {
		t.Errorf("run %+v", run)
	}
	var list []watch.CheckRun
	if code := s.call(t, "GET", fmt.Sprintf("/api/runs?watch=%d&since=7d&limit=5", w.ID), "", &list); code != http.StatusOK || len(list) != 1 {
		t.Errorf("runs: %d %+v", code, list)
	}
	if code := s.call(t, "GET", "/api/runs?since=yesterday", "", &e); code != http.StatusBadRequest {
		t.Errorf("bad since: %d", code)
	}
	if code := s.call(t, "POST", fmt.Sprintf("/api/runs/%d/cancel", resp.Run), "", &e); code != http.StatusNotFound {
		t.Errorf("cancel a finished run: %d", code)
	}
}

func TestAPIToken(t *testing.T) {
	s := newTestServer(t, "s3cret")
	for auth, want := range map[string]int{"": http.StatusUnauthorized, "Bearer nope": http.StatusUnauthorized, "Bearer s3cret": http.StatusOK} {
		req, _ := http.NewRequest("GET", s.URL+"/api/watches", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%q: %d, want %d", auth, resp.StatusCode, want)
		}
	}
}

// TestAuthWeb: with a token the web UI asks for it (basic auth), the API takes it both ways.
func TestAuthWeb(t *testing.T) {
	s := newTestServer(t, "s3cret")
	mux := http.NewServeMux()
	s.api.Register(mux)
	mux.Handle("/", s.api.AuthWeb(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})))
	for _, tc := range []struct {
		path, user, password string
		want                 int
	}{
		{"/", "", "", http.StatusUnauthorized},
		{"/api/image?url=http://10.0.0.1/", "", "", http.StatusUnauthorized},
		{"/", "x", "nope", http.StatusUnauthorized},
		{"/", "x", "s3cret", http.StatusOK},
		{"/api/watches", "", "s3cret", http.StatusOK},
	} {
		req := httptest.NewRequest("GET", tc.path, nil)
		if tc.password != "" {
			req.SetBasicAuth(tc.user, tc.password)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s %q: %d, want %d", tc.path, tc.password, rec.Code, tc.want)
		}
		if rec.Code == http.StatusUnauthorized && tc.path == "/" && !strings.HasPrefix(rec.Header().Get("WWW-Authenticate"), "Basic") {
			t.Errorf("%s: challenge %q", tc.path, rec.Header().Get("WWW-Authenticate"))
		}
	}
}

// TestCheckWhileStopping: a check asked once the server waits for the checks is refused.
func TestCheckWhileStopping(t *testing.T) {
	s := newTestServer(t, "")
	s.api.Wait()
	if code := s.call(t, "POST", "/api/check", `{}`, nil); code != http.StatusServiceUnavailable {
		t.Errorf("check while stopping: %d", code)
	}
}

func TestHostCheck(t *testing.T) {
	s := newTestServer(t, "")
	mux := http.NewServeMux()
	s.api.Register(mux)
	h := web.Guard(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 6977}, mux)
	for host, want := range map[string]int{"127.0.0.1:6977": http.StatusOK, "localhost:6977": http.StatusOK, "evil.example:6977": http.StatusMisdirectedRequest} {
		req := httptest.NewRequest("GET", "/api/watches", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("%s: %d, want %d", host, rec.Code, want)
		}
	}
}

// TestCrossSite: a page of another site can't use the API (no-cors POSTs need no preflight).
func TestCrossSite(t *testing.T) {
	s := newTestServer(t, "")
	mux := http.NewServeMux()
	s.api.Register(mux)
	h := web.Guard(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 6977}, mux)
	for _, tc := range []struct {
		method, path, origin, contentType string
		want                              int
	}{
		{"POST", "/api/check", "", "text/plain", http.StatusUnsupportedMediaType},
		{"POST", "/api/check", "", "", http.StatusUnsupportedMediaType},
		{"POST", "/api/check", "https://evil.example", "application/json", http.StatusForbidden},
		{"POST", "/api/check", "null", "application/json", http.StatusForbidden},
		{"DELETE", "/api/watches/1", "https://evil.example", "", http.StatusForbidden},
		// safe methods pass: without CORS headers the page can't read the answer
		{"GET", "/api/watches", "https://evil.example", "", http.StatusOK},
		{"POST", "/api/watches", "http://127.0.0.1:6977", "application/json; charset=utf-8", http.StatusBadRequest},
		{"GET", "/api/watches", "http://127.0.0.1:6977", "", http.StatusOK},
	} {
		// the event stream ends with the request
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		req := httptest.NewRequestWithContext(ctx, tc.method, tc.path, strings.NewReader(`{"dry_run":true,"nope":1}`))
		req.Host = "127.0.0.1:6977"
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		if tc.contentType != "" {
			req.Header.Set("Content-Type", tc.contentType)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s %s origin %q type %q: %d, want %d", tc.method, tc.path, tc.origin, tc.contentType, rec.Code, tc.want)
		}
	}
	runs, err := s.svc.Runs(context.Background(), watch.RunFilter{})
	if err != nil || len(runs) != 0 {
		t.Errorf("runs %v %v", runs, err)
	}
}

func TestRemote(t *testing.T) {
	s := newTestServer(t, "tok")
	ctx := context.Background()
	r := watch.NewRemote(s.URL, "tok")
	r.PollInterval = 10 * time.Millisecond

	from := float32(1)
	w, err := r.Add(ctx, watch.AddOptions{Provider: "fake", Query: "One-Punch Man", From: &from, Schedule: "@hourly"})
	if err != nil || w.MangaID != "opm" || w.Schedule != "@hourly" {
		t.Fatalf("add: %+v, %v", w, err)
	}
	if _, err := r.Add(ctx, watch.AddOptions{Provider: "fake", MangaID: "opm"}); !errors.Is(err, watch.ErrExists) {
		t.Errorf("add twice: %v", err)
	}
	if _, err := r.Get(ctx, 999); !errors.Is(err, watch.ErrNotFound) {
		t.Errorf("get missing: %v", err)
	}
	if list, err := r.List(ctx); err != nil || len(list) != 1 {
		t.Errorf("list: %+v, %v", list, err)
	}
	if w, err = r.SetEnabled(ctx, w.ID, false); err != nil || w.Enabled {
		t.Errorf("pause: %+v, %v", w, err)
	}
	if w, err = r.SetEnabled(ctx, w.ID, true); err != nil || !w.Enabled {
		t.Errorf("resume: %+v, %v", w, err)
	}
	schedule, gaps := "", true
	anilistID := 21087
	if w, err = r.Update(ctx, w.ID, watch.UpdateOptions{Schedule: &schedule, Gaps: &gaps, AnilistID: &anilistID}); err != nil || w.Schedule != "" || !w.Gaps ||
		w.AnilistID != 21087 || len(w.Bindings) != 1 {
		t.Errorf("update: %+v, %v", w, err)
	}
	if _, err := r.Relink(ctx, w.ID, watch.RelinkOptions{MangaID: "nope"}); err == nil || errors.Is(err, watch.ErrExists) {
		t.Errorf("re-link to a missing manga: %v", err)
	}
	if w, err = r.Relink(ctx, w.ID, watch.RelinkOptions{Query: "Dandadan"}); err != nil || w.MangaID != "dan" || w.Title != "Dandadan" ||
		w.AnilistID != 21087 {
		t.Errorf("re-link: %+v, %v", w, err)
	}
	if w, err = r.Relink(ctx, w.ID, watch.RelinkOptions{MangaID: "opm"}); err != nil || w.MangaID != "opm" {
		t.Errorf("re-link back: %+v, %v", w, err)
	}
	if stale, err := r.Stale(ctx, 0); err != nil || len(stale) != 1 {
		t.Errorf("stale: %+v, %v", stale, err)
	}
	if st, err := r.Status(ctx); err != nil || !st.Server || st.Checking {
		t.Errorf("status: %+v, %v", st, err)
	}

	dry, err := r.Check(ctx, nil, watch.CheckOptions{DryRun: true})
	if err != nil || !dry.DryRun || len(dry.Results) != 1 || len(dry.Results[0].Chapters) != 2 {
		t.Errorf("dry run: %+v, %v", dry, err)
	}
	var live []string
	report, err := r.Check(ctx, []int64{w.ID}, watch.CheckOptions{OnLive: func(e watch.LiveEvent) {
		if e.Kind != watch.LiveProgress {
			live = append(live, e.Kind)
		}
	}})
	if err != nil || report.ID == 0 || report.Finished.IsZero() || report.NewChapters != 2 || len(report.Results) != 1 || report.Results[0].Status != storage.StatusNew {
		t.Fatalf("check: %+v, %v", report, err)
	}
	// the check's events streamed, up to its end
	if len(live) == 0 || live[0] != watch.LiveRunStart || live[len(live)-1] != watch.LiveRunDone || !slices.Contains(live, watch.LiveFound) {
		t.Errorf("live events %v", live)
	}
	runs, err := r.Runs(ctx, watch.RunFilter{Watch: w.ID, Since: time.Now().Add(-time.Hour), Limit: 10})
	if err != nil || len(runs) != 1 || runs[0].ID != report.ID {
		t.Errorf("runs: %+v, %v", runs, err)
	}
	if runs, err := r.Runs(ctx, watch.RunFilter{Provider: w.Provider, MangaID: w.MangaID}); err != nil || len(runs) != 1 {
		t.Errorf("runs of the manga: %+v, %v", runs, err)
	}
	if runs, err := r.Runs(ctx, watch.RunFilter{Provider: "other"}); err != nil || len(runs) != 0 {
		t.Errorf("runs of another provider: %+v, %v", runs, err)
	}
	if run, err := r.RunDetails(ctx, report.ID); err != nil || len(run.Results) != 1 {
		t.Errorf("run: %+v, %v", run, err)
	}
	if _, err := r.RunDetails(ctx, 999); !errors.Is(err, watch.ErrNotFound) {
		t.Errorf("missing run: %v", err)
	}

	// busy, then cancelled: the server's check stops and is reported
	s.src.n["opm"] = 5
	block := make(chan struct{})
	started := make(chan struct{})
	s.src.mu.Lock()
	s.src.block, s.src.started = block, started
	s.src.mu.Unlock()
	cctx, cancel := context.WithCancel(ctx)
	type result struct {
		report watch.Report
		err    error
	}
	done := make(chan result, 1)
	go func() {
		report, err := r.Check(cctx, nil, watch.CheckOptions{})
		done <- result{report, err}
	}()
	<-started
	if _, err := r.Check(ctx, nil, watch.CheckOptions{}); !errors.Is(err, watch.ErrBusy) {
		t.Errorf("second check: %v", err)
	}
	cancel()
	res := <-done
	if res.err != nil || res.report.Finished.IsZero() || res.report.NewChapters != 0 {
		t.Errorf("cancelled check: %+v, %v", res.report, res.err)
	}
	if err := r.Remove(ctx, w.ID); err != nil {
		t.Error(err)
	}
	if err := r.Remove(ctx, w.ID); !errors.Is(err, watch.ErrNotFound) {
		t.Errorf("remove twice: %v", err)
	}
	// the history stays after the watch is removed, a run of it can be removed
	run, _ := r.RunDetails(ctx, report.ID)
	if removed, err := r.RemoveRun(ctx, run.Results[0].ID); err != nil || !removed.CheckRunRemoved || removed.Run.ID != run.Results[0].ID {
		t.Errorf("remove run: %+v, %v", removed, err)
	}
	if _, err := r.RemoveRun(ctx, run.Results[0].ID); !errors.Is(err, watch.ErrNotFound) {
		t.Errorf("remove run twice: %v", err)
	}

	bad := watch.NewRemote(s.URL, "")
	var apiErr *watch.APIError
	if _, err := bad.List(ctx); !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized {
		t.Errorf("no token: %v", err)
	}
}

func TestErrorBody(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, fmt.Errorf("check: %w", watch.ErrBusy))
	var e watch.ErrorResponse
	if err := json.NewDecoder(bytes.NewReader(rec.Body.Bytes())).Decode(&e); err != nil || rec.Code != http.StatusConflict || e.Code != watch.CodeBusy || !strings.Contains(e.Error, "busy") {
		t.Errorf("%d %+v %v", rec.Code, e, err)
	}
}

// readSSE reads the events of an event stream until stop says so.
func readSSE(t *testing.T, body io.Reader, stop func(id string, e watch.LiveEvent) bool) {
	t.Helper()
	sc := bufio.NewScanner(body)
	id := ""
	for sc.Scan() {
		line := sc.Text()
		if v, ok := strings.CutPrefix(line, "id: "); ok {
			id = v
		}
		if v, ok := strings.CutPrefix(line, "data: "); ok {
			var e watch.LiveEvent
			if err := json.Unmarshal([]byte(v), &e); err != nil {
				t.Fatalf("event %q: %s", v, err)
			}
			if fmt.Sprint(e.Seq) != id {
				t.Errorf("id %s of event %d", id, e.Seq)
			}
			if stop(id, e) {
				return
			}
		}
	}
	t.Fatalf("stream ended: %v", sc.Err())
}

func TestAPIEvents(t *testing.T) {
	s := newTestServer(t, "tok")
	ctx := context.Background()
	r := watch.NewRemote(s.URL, "tok")
	r.PollInterval = 10 * time.Millisecond
	w, err := r.Add(ctx, watch.AddOptions{Provider: "fake", MangaID: "opm"})
	if err != nil {
		t.Fatal(err)
	}
	block := make(chan struct{})
	started := make(chan struct{})
	s.src.mu.Lock()
	s.src.block, s.src.started = block, started
	s.src.mu.Unlock()
	var resp watch.CheckResponse
	req, _ := http.NewRequest("POST", s.URL+"/api/check", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.NewDecoder(res.Body).Decode(&resp)
	res.Body.Close()
	<-started

	// connected mid-run: what happened so far
	get := func(since string) *http.Response {
		req, _ := http.NewRequest("GET", s.URL+"/api/events"+since, nil)
		req.Header.Set("Authorization", "Bearer tok")
		res, err := http.DefaultClient.Do(req)
		if err != nil || res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "text/event-stream" {
			t.Fatalf("events: %v %+v", err, res)
		}
		return res
	}
	res = get("")
	var kinds []string
	foundID := ""
	readSSE(t, res.Body, func(id string, e watch.LiveEvent) bool {
		if e.Run != resp.Run {
			t.Errorf("event of run %d", e.Run)
		}
		kinds = append(kinds, e.Kind)
		if e.Kind == watch.LiveFound {
			foundID = id
			if e.Watch != w.ID || e.Provider != "fake" || !strings.Contains(e.Message, "3 to download") {
				t.Errorf("found %+v", e)
			}
		}
		return e.Kind == watch.LiveFound
	})
	res.Body.Close()
	if !slices.Equal(kinds, []string{watch.LiveRunStart, watch.LiveProviderStart, watch.LiveStart, watch.LiveFound}) {
		t.Errorf("kinds %v", kinds)
	}

	// the remote subscription, and a reconnection after the found event
	sub, err := r.Subscribe(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	res = get("?since=" + foundID)
	defer res.Body.Close()
	close(block)
	first := true
	readSSE(t, res.Body, func(id string, e watch.LiveEvent) bool {
		if first && e.Kind != watch.LiveDone {
			t.Errorf("after the found event: %+v", e)
		}
		first = false
		return e.Kind == watch.LiveRunDone
	})
	var subKinds []string
	for e := range sub {
		subKinds = append(subKinds, e.Kind)
		if e.Kind == watch.LiveRunDone {
			break
		}
	}
	if len(subKinds) != 7 || subKinds[0] != watch.LiveRunStart || subKinds[4] != watch.LiveDone {
		t.Errorf("subscription %v", subKinds)
	}

	if _, err := watch.NewRemote(s.URL, "").Subscribe(ctx); err == nil {
		t.Error("subscribed without the token")
	}
}

// TestCLIWhileServing: a `watch check` of another process (another Service on the same
// data db) can't check the provider the server checks, and sees it only as locked.
func TestCLIWhileServing(t *testing.T) {
	s := newTestServer(t, "")
	var w watch.Watch
	if code := s.call(t, "POST", "/api/watches", `{"provider":"fake","manga_id":"opm"}`, &w); code != http.StatusCreated {
		t.Fatal(code)
	}
	block := make(chan struct{})
	started := make(chan struct{})
	s.src.mu.Lock()
	s.src.block, s.src.started = block, started
	s.src.mu.Unlock()
	var resp watch.CheckResponse
	if code := s.call(t, "POST", "/api/check", `{}`, &resp); code != http.StatusAccepted {
		t.Fatal(code)
	}
	<-started
	cli := *s.svc
	cli.Events = watch.NewHub()
	ctx := context.Background()
	if _, err := cli.Check(ctx, []int64{w.ID}, watch.CheckOptions{}); !errors.Is(err, watch.ErrBusy) {
		t.Errorf("cli check while serving: %v", err)
	}
	st, err := cli.Status(ctx)
	if ps, ok := st.Provider("fake"); err != nil || !ok || ps.Watch != 0 || ps.PID == 0 {
		t.Errorf("cli status %+v %v", st, err)
	}
	close(block)
	waitRun(t, s, resp.Run)
	if _, err := cli.Check(ctx, []int64{w.ID}, watch.CheckOptions{}); err != nil {
		t.Errorf("cli check after: %v", err)
	}
}

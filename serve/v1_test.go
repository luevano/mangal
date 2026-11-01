package serve

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/web/apiv1"
	"github.com/spf13/afero"
)

// TestV1Meta: the index lists every operation with its scope, the spec is served, the
// status has the API version and the features, also on its old path.
func TestV1Meta(t *testing.T) {
	s := newTestServer(t, "")
	var index apiv1.Index
	if code := s.call(t, "GET", "/api/v1/", "", &index); code != http.StatusOK || index.ApiVersion != APIVersion || len(index.Operations) < 50 {
		t.Fatalf("index %d %+v", code, index)
	}
	for _, o := range index.Operations {
		if o.OperationId == "mergeLibraryMangas" && (o.Scope != ScopeManage || o.Plan == nil || !*o.Plan || o.Method != "POST") {
			t.Errorf("merge %+v", o)
		}
	}
	res, err := http.Get(s.URL + "/api/v1/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "application/yaml" || !strings.Contains(string(b), "operationId: getStatus") {
		t.Errorf("spec %d %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	for _, path := range []string{"/api/v1/status", "/api/status"} {
		var st apiv1.Status
		if code := s.call(t, "GET", path, "", &st); code != http.StatusOK || !st.Server || st.ApiVersion != APIVersion ||
			len(st.Features) == 0 || len(st.Scopes) != len(Scopes) || st.Auth == "" {
			t.Errorf("%s: %d %+v", path, code, st)
		}
	}
	var e ErrorResponse
	if code := s.call(t, "GET", "/api/v1/nope", "", &e); code != http.StatusNotFound || e.Code != "not_found" {
		t.Errorf("no route: %d %+v", code, e)
	}
	if code := s.call(t, "GET", "/api/v1/watches/abc", "", &e); code != http.StatusBadRequest || e.Code != CodeInvalid {
		t.Errorf("bad id: %d %+v", code, e)
	}
}

// TestV1ReadEndpoints: the library, health, jobs, logs, history, bindings, config,
// providers answer.
func TestV1ReadEndpoints(t *testing.T) {
	s, _, _ := newHealthServer(t)
	var mangas apiv1.LibraryMangaList
	if code := s.call(t, "GET", "/api/v1/library/mangas?q=punch", "", &mangas); code != http.StatusOK || mangas.Total != 1 || mangas.Mangas[0].Title != "One-Punch Man" {
		t.Fatalf("mangas %d %+v", code, mangas)
	}
	if code := s.call(t, "GET", "/api/v1/library/mangas?q=nothing", "", &mangas); code != http.StatusOK || mangas.Total != 0 || mangas.Mangas == nil {
		t.Errorf("no mangas %d %+v", code, mangas)
	}
	var detail apiv1.LibraryMangaDetail
	if code := s.call(t, "GET", "/api/v1/library/mangas/1", "", &detail); code != http.StatusOK || detail.Manga.Id != 1 || !detail.Missing {
		t.Errorf("manga %d %+v", code, detail)
	}
	if code := s.call(t, "GET", "/api/v1/library/mangas/99", "", nil); code != http.StatusNotFound {
		t.Errorf("missing manga %d", code)
	}
	var chapters []apiv1.LibraryChapter
	if code := s.call(t, "GET", "/api/v1/library/mangas/1/chapters", "", &chapters); code != http.StatusOK || len(chapters) != 3 {
		t.Errorf("chapters %d %+v", code, chapters)
	}
	var downloads apiv1.DownloadList
	if code := s.call(t, "GET", "/api/v1/library/downloads?provider=fake&limit=2", "", &downloads); code != http.StatusOK || downloads.Total != 3 || len(downloads.Downloads) != 2 {
		t.Errorf("downloads %d %+v", code, downloads)
	}
	for _, path := range []string{"/api/v1/library/duplicates", "/api/v1/library/trash", "/api/v1/ops", "/api/v1/health",
		"/api/v1/history", "/api/v1/bindings", "/api/v1/logs/files", "/api/v1/logs?limit=5", "/api/v1/formats", "/api/v1/watches", "/api/v1/runs"} {
		if code := s.call(t, "GET", path, "", nil); code != http.StatusOK {
			t.Errorf("%s: %d", path, code)
		}
	}
	// no job manager in this server
	if code := s.call(t, "GET", "/api/v1/jobs", "", nil); code != http.StatusServiceUnavailable {
		t.Errorf("jobs: %d", code)
	}
	var cfg []apiv1.ConfigEntry
	if code := s.call(t, "GET", "/api/v1/config", "", &cfg); code != http.StatusOK || len(cfg) == 0 {
		t.Fatalf("config %d", code)
	}
	for _, e := range cfg {
		if e.Key == "serve.token" && !e.Secret {
			t.Errorf("serve.token isn't a secret: %+v", e)
		}
		if e.Key == "watch.schedule" && e.Applies != "now" {
			t.Errorf("watch.schedule %+v", e)
		}
	}
	var e ErrorResponse
	if code := s.call(t, "PUT", "/api/v1/config/notification.discord.webhook_url", `{"value":"https://discord.com/api/webhooks/1/x"}`, &e); code != http.StatusForbidden {
		t.Errorf("a secret over HTTP: %d %+v", code, e)
	}
	if code := s.call(t, "PUT", "/api/v1/config/nope", `{"value":1}`, &e); code != http.StatusNotFound {
		t.Errorf("unknown key: %d %+v", code, e)
	}
	// every setting is an admin's to change, a writer is refused before the key is looked up
	addUser(t, s, "writer", "a long password", ScopeRead, ScopeDownload, ScopeManage)
	h := s.handler(t)
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte("writer:a long password"))
	for _, key := range []string{"providers.allow_private_addresses", "providers.private_allowlist", "data.path", "providers.path", "cache.path", "serve.auth", "providers.headless.flaresolverr_url", "providers.mirrors"} {
		if rec := do(h, "PUT", "/api/v1/config/"+key, `{"value":"true"}`, "127.0.0.1:1", auth, nil); rec.Code != http.StatusForbidden {
			t.Errorf("%s by a writer: %d %s", key, rec.Code, rec.Body)
		}
	}
}

// TestV1PlanApply: plan, apply needs the hash, a stale hash is refused with the new
// plan, then the operation is undone the same way; the op events (from the journal, as
// any process's) are streamed, with who did it.
func TestV1PlanApply(t *testing.T) {
	s, _, _ := newHealthServer(t)
	s.api.watchData(t.Context(), s.src.library.DB)
	events := s.subscribe(t, "op")

	var plan apiv1.ChangeResponse
	if code := s.call(t, "POST", "/api/v1/library/forget", `{"mangas":[1]}`, &plan); code != http.StatusOK || plan.Applied || plan.Plan == nil ||
		!strings.HasPrefix(plan.Plan.Hash, "sha256:") || len(plan.Plan.Lines) == 0 {
		t.Fatalf("plan %d %+v", code, plan)
	}
	var e ErrorResponse
	if code := s.call(t, "POST", "/api/v1/library/forget?apply=1", `{"mangas":[1]}`, &e); code != http.StatusBadRequest || e.Code != CodeInvalid {
		t.Errorf("apply without expect: %d %+v", code, e)
	}
	// the library changes: the plan is stale
	if err := s.src.library.RecordDownload("/lib", "One-Punch Man", storage.Download{Path: "/lib/One-Punch Man/5.cbz",
		Provider: "fake", MangaID: "opm", MangaTitle: "One-Punch Man", Chapter: 5, Format: "CBZ"}); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"mangas":[1],"expect":%q}`, plan.Plan.Hash)
	if code := s.call(t, "POST", "/api/v1/library/forget?apply=1", body, &e); code != http.StatusConflict || e.Code != CodePlanChanged || e.Plan == nil || e.Plan.Hash == plan.Plan.Hash {
		t.Fatalf("stale plan: %d %+v", code, e)
	}
	var applied apiv1.ChangeResponse
	body = fmt.Sprintf(`{"mangas":[1],"expect":%q}`, e.Plan.Hash)
	if code := s.call(t, "POST", "/api/v1/library/forget?apply=1", body, &applied); code != http.StatusOK || !applied.Applied || applied.Result == nil ||
		applied.Result.OpID == 0 || applied.Undo == nil {
		t.Fatalf("apply %d %+v", code, applied)
	}
	if code := s.call(t, "GET", "/api/v1/library/mangas/1", "", nil); code != http.StatusNotFound {
		t.Errorf("forgotten manga: %d", code)
	}
	for _, kind := range []string{"started", "done"} {
		ev := nextEvent(t, events)
		data, _ := ev.Data.(map[string]any)
		if ev.Topic != TopicOp || ev.Kind != kind || data["id"] != float64(applied.Result.OpID) || data["origin"] != "api: local (this config's mangal)" {
			t.Errorf("event %+v, want %s", ev, kind)
		}
	}

	undo := fmt.Sprintf("/api/v1/ops/%d/undo", applied.Result.OpID)
	var undoPlan apiv1.ChangeResponse
	if code := s.call(t, "POST", undo, "", &undoPlan); code != http.StatusOK || undoPlan.Plan == nil || undoPlan.Plan.Kind != core.KindUndo {
		t.Fatalf("undo plan %d %+v", code, undoPlan)
	}
	var undone apiv1.ChangeResponse
	if code := s.call(t, "POST", undo+"?apply=true", fmt.Sprintf(`{"expect":%q}`, undoPlan.Plan.Hash), &undone); code != http.StatusOK || !undone.Applied {
		t.Fatalf("undo %d %+v", code, undone)
	}
	if code := s.call(t, "GET", "/api/v1/library/mangas/1", "", nil); code != http.StatusOK {
		t.Errorf("undone manga: %d", code)
	}
	if ev := nextEvent(t, events); ev.Kind != "undone" {
		t.Errorf("event %+v, want undone", ev)
	}
	var ops []apiv1.Op
	if code := s.call(t, "GET", "/api/v1/ops?limit=1", "", &ops); code != http.StatusOK || len(ops) != 1 || ops[0].Undone == nil {
		t.Errorf("ops %d %+v", code, ops)
	}

	// emptying the trash can't be undone: it needs a confirm
	c, err := s.api.services(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := afero.WriteFile(c.Env().FS, "/lib/"+library.TrashDir+"/2026-10-15 01-00-00/One-Punch Man/1.cbz", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	var trash apiv1.ChangeResponse
	if code := s.call(t, "POST", "/api/v1/library/trash/empty", "", &trash); code != http.StatusOK || !trash.Plan.Irreversible {
		t.Fatalf("trash plan %d %+v", code, trash)
	}
	body = fmt.Sprintf(`{"expect":%q}`, trash.Plan.Hash)
	if code := s.call(t, "POST", "/api/v1/library/trash/empty?apply=1", body, &e); code != http.StatusPreconditionRequired || e.Code != CodeConfirm || e.Confirm != ConfirmEmptyTrash {
		t.Errorf("no confirm: %d %+v", code, e)
	}
	// a forced undo too
	if code := s.call(t, "POST", undo+"?apply=1", `{"expect":"x","force":true}`, &e); code != http.StatusPreconditionRequired || e.Confirm != ConfirmForceUndo {
		t.Errorf("forced undo: %d %+v", code, e)
	}
}

// subscribe streams the events of the topics (until the test ends).
func nextEvent(t *testing.T, events <-chan Event) Event {
	t.Helper()
	select {
	case ev := <-events:
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("no event")
		return Event{}
	}
}

func (s *testServer) subscribe(t *testing.T, topics string) <-chan Event {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", s.URL+"/api/v1/events?topics="+topics, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("events %d %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	ch := make(chan Event, 16)
	go func() {
		defer res.Body.Close()
		readEvents(res.Body, func(e Event) { ch <- e })
	}()
	// the stream is open once its comment came; nothing to wait for here
	time.Sleep(50 * time.Millisecond)
	return ch
}

func readEvents(body io.Reader, fn func(Event)) {
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, err := body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		for {
			i := strings.Index(string(buf), "\n\n")
			if i < 0 {
				break
			}
			block := string(buf[:i])
			buf = buf[i+2:]
			for line := range strings.SplitSeq(block, "\n") {
				if v, ok := strings.CutPrefix(line, "data: "); ok {
					var e Event
					if json.Unmarshal([]byte(v), &e) == nil {
						fn(e)
					}
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// TestV1Events: topics filter, Last-Event-ID replays what came after it.
func TestV1Events(t *testing.T) {
	s := newTestServer(t, "")
	s.api.Events.Publish(TopicLog, "info", "one", nil)
	s.api.Events.Publish(TopicOp, "done", "two", nil)
	s.api.Events.Publish(TopicLog, "info", "three", nil)
	if code := s.call(t, "GET", "/api/v1/events?topics=nope", "", nil); code != http.StatusBadRequest {
		t.Errorf("unknown topic: %d", code)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", s.URL+"/api/v1/events?topics=log", nil)
	req.Header.Set("Last-Event-ID", "1")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	got := make(chan Event, 4)
	go readEvents(res.Body, func(e Event) { got <- e })
	select {
	case e := <-got:
		if e.Seq != 3 || e.Message != "three" || e.Topic != TopicLog {
			t.Errorf("event %+v", e)
		}
	case <-ctx.Done():
		t.Fatal("no event")
	}
}

// TestV1Jobs: a scan runs as a job, its events stream, a finished job can be removed.
func TestV1Jobs(t *testing.T) {
	s, _, _ := newHealthServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s.api.Jobs = jobs.New(ctx)
	go s.api.Events.forwardJobs(ctx, s.api.Jobs)
	events := s.subscribe(t, "job")
	var job apiv1.Job
	if code := s.call(t, "POST", "/api/v1/library/scan", `{}`, &job); code != http.StatusAccepted || job.Kind != "scan" {
		t.Fatalf("scan %d %+v", code, job)
	}
	deadline := time.Now().Add(5 * time.Second)
	for job.Status != "done" && job.Status != "failed" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		s.call(t, "GET", fmt.Sprintf("/api/v1/jobs/%d", job.Id), "", &job)
	}
	// the memory file system has no /lib: the scan says the drive isn't there
	if job.Status != "failed" || job.Error == nil || !strings.Contains(*job.Error, "not mounted") {
		t.Errorf("job %+v", job)
	}
	var kinds []string
	for len(kinds) < 2 {
		select {
		case e := <-events:
			kinds = append(kinds, e.Kind)
		case <-time.After(3 * time.Second):
			t.Fatalf("events %v", kinds)
		}
	}
	if kinds[0] != "added" || kinds[len(kinds)-1] != "finished" {
		t.Errorf("events %v", kinds)
	}
	var list []apiv1.Job
	if code := s.call(t, "GET", "/api/v1/jobs", "", &list); code != http.StatusOK || len(list) != 1 {
		t.Errorf("jobs %d %+v", code, list)
	}
	var e ErrorResponse
	if code := s.call(t, "POST", fmt.Sprintf("/api/v1/jobs/%d/retry", job.Id), "", &e); code != http.StatusConflict || e.Code != CodeNothingToDo {
		t.Errorf("retry a scan: %d %+v", code, e)
	}
	if code := s.call(t, "POST", fmt.Sprintf("/api/v1/jobs/%d/cancel", job.Id), "", &e); code != http.StatusConflict {
		t.Errorf("cancel a finished job: %d %+v", code, e)
	}
	if code := s.call(t, "DELETE", fmt.Sprintf("/api/v1/jobs/%d", job.Id), "", nil); code != http.StatusNoContent {
		t.Errorf("remove: %d", code)
	}
	if code := s.call(t, "GET", fmt.Sprintf("/api/v1/jobs/%d", job.Id), "", nil); code != http.StatusNotFound {
		t.Errorf("removed: %d", code)
	}
	if code := s.call(t, "POST", "/api/v1/downloads", `{"provider":"nope","manga_id":"x","chapters":"all"}`, &e); code != http.StatusNotFound {
		t.Errorf("download from an unknown provider: %d %+v", code, e)
	}
}

// TestOpWatch: another process's operation (only in the journal) is told started, done
// and undone, the ones there before the watch aren't.
func TestOpWatch(t *testing.T) {
	s := newTestServer(t, "")
	j := storage.Journal{DB: s.src.library.DB, Origin: "tui"}
	if _, err := j.Begin("merge", "old", ""); err != nil {
		t.Fatal(err)
	}
	w, err := newOpWatch(j)
	if err != nil {
		t.Fatal(err)
	}
	kinds := func() (out []string) {
		events, err := w.changes()
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range events {
			out = append(out, e.kind+" "+e.op.Summary+" "+e.op.Origin)
		}
		return out
	}
	op, err := j.Begin("rename", "new", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := kinds(); !slices.Equal(got, []string{"started new tui"}) {
		t.Errorf("started: %q", got)
	}
	if err := op.Finish(nil); err != nil {
		t.Fatal(err)
	}
	if err := j.MarkUndone(*op); err != nil {
		t.Fatal(err)
	}
	if got := kinds(); !slices.Equal(got, []string{"done new tui", "undone new tui"}) {
		t.Errorf("done, undone: %q", got)
	}
	if got := kinds(); len(got) != 0 {
		t.Errorf("nothing new: %q", got)
	}

	// an operation however old: no window of the newest ones
	for i := range 150 {
		if _, err := j.Begin("rename", fmt.Sprintf("filler %d", i), ""); err != nil {
			t.Fatal(err)
		}
	}
	if got := kinds(); len(got) != 150 {
		t.Errorf("fillers: %d events", len(got))
	}
	first, ok, err := j.Op(1)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := j.MarkUndone(first); err != nil {
		t.Fatal(err)
	}
	if got := kinds(); !slices.Equal(got, []string{"undone old tui"}) {
		t.Errorf("undone oldest: %q", got)
	}
}

// TestV1DismissPick: a pick is dismissed through plan and apply, then there's nothing to
// dismiss.
func TestV1DismissPick(t *testing.T) {
	s, _, _ := newHealthServer(t)
	if _, err := s.src.library.SetLinkPick(storage.LinkPick{Manga: 1, Source: "kt", Candidates: []storage.LinkCandidate{{ID: "1", From: "kitsu"}, {ID: "2", From: "fake"}}}); err != nil {
		t.Fatal(err)
	}
	var plan apiv1.ChangeResponse
	if code := s.call(t, "POST", "/api/v1/library/picks/dismiss", `{"manga":1,"source":"kt"}`, &plan); code != http.StatusOK || plan.Plan == nil || plan.Plan.Nothing {
		t.Fatalf("plan %d %+v", code, plan)
	}
	var applied apiv1.ChangeResponse
	if code := s.call(t, "POST", "/api/v1/library/picks/dismiss?apply=1", fmt.Sprintf(`{"manga":1,"source":"kt","expect":%q}`, plan.Plan.Hash), &applied); code != http.StatusOK || !applied.Applied {
		t.Fatalf("apply %d %+v", code, applied)
	}
	if pick, _, err := s.src.library.LinkPick(1, "kt"); err != nil || !pick.Dismissed {
		t.Errorf("pick %+v %v", pick, err)
	}
	if code := s.call(t, "POST", "/api/v1/library/picks/dismiss", `{"manga":1,"source":"kt"}`, &plan); code != http.StatusOK || !plan.Plan.Nothing {
		t.Errorf("again %d %+v", code, plan)
	}
}

// TestV1R3Endpoints: the missing folders in the list, a manga's links with its picks,
// a single manga's scan, cancel-all.
func TestV1R3Endpoints(t *testing.T) {
	s, _, _ := newHealthServer(t)
	var mangas apiv1.LibraryMangaList
	if code := s.call(t, "GET", "/api/v1/library/mangas?missing=true", "", &mangas); code != http.StatusOK || mangas.Missing == nil || !slices.Contains(*mangas.Missing, int64(1)) {
		t.Errorf("missing %d %+v", code, mangas.Missing)
	}
	var plain apiv1.LibraryMangaList
	if code := s.call(t, "GET", "/api/v1/library/mangas", "", &plain); code != http.StatusOK || plain.Missing != nil {
		t.Errorf("missing without asking %d %+v", code, plain.Missing)
	}
	if _, err := s.src.library.SetLinkPick(storage.LinkPick{Manga: 1, Source: "kt", Candidates: []storage.LinkCandidate{{ID: "1", From: "kitsu"}}}); err != nil {
		t.Fatal(err)
	}
	var links apiv1.LibraryLinks
	if code := s.call(t, "GET", "/api/v1/library/mangas/1/links", "", &links); code != http.StatusOK || links.Info.Manga.ID != 1 || len(links.Picks) != 1 || links.Picks[0].Source != "kt" {
		t.Errorf("links %d %+v", code, links)
	}
	if code := s.call(t, "GET", "/api/v1/library/mangas/99/links", "", nil); code != http.StatusNotFound {
		t.Errorf("links of no manga %d", code)
	}
	var scan apiv1.MangaScan
	if code := s.call(t, "POST", "/api/v1/library/mangas/1/scan", "", &scan); code != http.StatusUnprocessableEntity {
		// the memory file system has no /lib: the drive isn't there
		t.Errorf("scan %d %+v", code, scan)
	}
	if code := s.call(t, "POST", "/api/v1/library/mangas/99/scan", "", nil); code != http.StatusNotFound {
		t.Errorf("scan of no manga %d", code)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s.api.Jobs = jobs.New(ctx)
	var n apiv1.Count
	if code := s.call(t, "POST", "/api/v1/jobs/cancel-all", "", &n); code != http.StatusOK || n.Count != 0 {
		t.Errorf("cancel-all %d %+v", code, n)
	}
}

// A download never comes in another format than the server's: the field is refused.
func TestDownloadFormatRefused(t *testing.T) {
	s := newTestServer(t, "")
	var e struct {
		Code, Error string
	}
	if code := s.call(t, "POST", "/api/v1/downloads", `{"provider":"fake","manga_id":"opm","chapters":"1","format":"pdf"}`, &e); code != http.StatusBadRequest {
		t.Fatalf("%d, %+v", code, e)
	}
	if !strings.Contains(e.Error, "download.format") {
		t.Errorf("error %q", e.Error)
	}
}

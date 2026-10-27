package serve

import (
	"context"
	"encoding/base64"
	"net/http"
	"testing"
	"time"

	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// TestV1JobKinds: the kinds listed, paused and resumed, run (nothing missing), their
// settings an admin's.
func TestV1JobKinds(t *testing.T) {
	s, _, _ := newHealthServer(t)
	m := jobs.New(context.Background())
	defer m.Shutdown(time.Second)
	s.api.Jobs = m

	var list []core.JobKind
	if code := s.call(t, "GET", "/api/v1/jobs/kinds", "", &list); code != http.StatusOK || len(list) != 5 || list[1].Kind != jobs.KindSourceCheck {
		t.Fatalf("list %d %+v", code, list)
	}
	var e ErrorResponse
	if code := s.call(t, "GET", "/api/v1/jobs/kinds/download", "", &e); code != http.StatusNotFound {
		t.Errorf("download is no kind: %d %+v", code, e)
	}
	var k core.JobKind
	if code := s.call(t, "POST", "/api/v1/jobs/kinds/sources/pause", "", &k); code != http.StatusOK || !k.Paused || k.NoNext != "paused" {
		t.Errorf("pause %d %+v", code, k)
	}
	if code := s.call(t, "GET", "/api/v1/jobs/kinds/sources", "", &k); code != http.StatusOK || !k.Paused || len(k.Providers) == 0 {
		t.Errorf("paused %d %+v", code, k)
	}
	if code := s.call(t, "POST", "/api/v1/jobs/kinds/sources/resume", "", &k); code != http.StatusOK || k.Paused {
		t.Errorf("resume %d %+v", code, k)
	}

	// the library's source has no address: a run of the missing ones looks it up
	var job apiv1.Job
	if code := s.call(t, "POST", "/api/v1/jobs/kinds/scan/run", `{"all":true}`, &job); code != http.StatusAccepted || job.Kind != "scan" {
		t.Fatalf("run %d %+v", code, job)
	}
	waitJob(context.Background(), m, job.Id)
	if code := s.call(t, "GET", "/api/v1/jobs/kinds/scan", "", &k); code != http.StatusOK || k.Last == nil || k.Last.Job != job.Id {
		t.Errorf("after the scan %d %+v", code, k.Last)
	}

	if code := s.call(t, "PUT", "/api/v1/jobs/kinds/urls/settings", `{"providers":{"fake":"off"},"delay_min":"2s"}`, &k); code != http.StatusOK || k.Providers[0].On {
		t.Errorf("settings %d %+v", code, k)
	}
	// the config file is the package's: back to the defaults
	defer s.call(t, "PUT", "/api/v1/jobs/kinds/urls/settings", `{"providers":{"fake":""},"delay_min":"1s"}`, nil)
	if code := s.call(t, "POST", "/api/v1/jobs/kinds/urls/run", "", &e); code != http.StatusConflict || e.Code != CodeNothingToDo {
		t.Errorf("nothing to do: %d %+v", code, e)
	}
	if code := s.call(t, "PUT", "/api/v1/jobs/kinds/urls/settings", `{"schedule":"soon"}`, &e); code != http.StatusBadRequest {
		t.Errorf("a bad interval: %d %+v", code, e)
	}

	addUser(t, s, "writer", "a long password", ScopeRead, ScopeDownload, ScopeManage)
	h := s.handler(t)
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte("writer:a long password"))
	if rec := do(h, "PUT", "/api/v1/jobs/kinds/urls/settings", `{"enabled":false}`, "127.0.0.1:1", auth, nil); rec.Code != http.StatusForbidden {
		t.Errorf("settings by a writer: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "PUT", "/api/v1/config/jobs.urls.enabled", `{"value":false}`, "127.0.0.1:1", auth, nil); rec.Code != http.StatusForbidden {
		t.Errorf("jobs key by a writer: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/v1/jobs/kinds/urls/pause", "", "127.0.0.1:1", auth, nil); rec.Code != http.StatusForbidden {
		t.Errorf("pause by a manager: %d %s", rec.Code, rec.Body)
	}
	// running one now is manage's
	if rec := do(h, "POST", "/api/v1/jobs/kinds/urls/run", "", "127.0.0.1:1", auth, nil); rec.Code == http.StatusForbidden {
		t.Errorf("run by a manager: %d %s", rec.Code, rec.Body)
	}
}

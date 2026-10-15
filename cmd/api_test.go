package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mangal api: the path under /api/v1, the token, -d makes a POST, an error exits
// non-zero, --sse prints the events' data, --list the spec's operations.
func TestAPICommand(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = append(got, r.Method+" "+r.URL.RequestURI()+" "+r.Header.Get("Authorization")+" "+string(body))
		switch r.URL.Path {
		case "/api/v1/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"server": true})
		case "/api/v1/library/merge":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"the library changed","code":"plan_changed"}`))
		case "/api/v1/events":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(": hi\n\nid: 1\ndata: {\"seq\":1}\n\nid: 2\ndata: {\"seq\":2}\n\n"))
		}
	}))
	defer srv.Close()
	t.Setenv("MANGAL_API_TOKEN", "t0ken")
	reset := func() {
		apiArgs.Data, apiArgs.SSE, apiArgs.List, apiArgs.Raw, apiArgs.Server = "", false, false, false, srv.URL
	}

	reset()
	out, err := runCmd(t, apiCmd, func() error { return apiCmd.RunE(apiCmd, []string{"status"}) })
	if err != nil || !strings.Contains(out, "\"server\": true") || got[0] != "GET /api/v1/status Bearer t0ken " {
		t.Errorf("status: %v %q %q", err, out, got)
	}

	reset()
	apiArgs.Data = `{"keep":1,"other":2}`
	out, err = runCmd(t, apiCmd, func() error { return apiCmd.RunE(apiCmd, []string{"library/merge?apply=1"}) })
	if err == nil || !strings.Contains(err.Error(), "plan_changed") || !strings.Contains(out, "\"code\": \"plan_changed\"") ||
		got[1] != `POST /api/v1/library/merge?apply=1 Bearer t0ken {"keep":1,"other":2}` {
		t.Errorf("merge: %v %q %q", err, out, got)
	}

	reset()
	apiArgs.Data = "{nope"
	if _, err := runCmd(t, apiCmd, func() error { return apiCmd.RunE(apiCmd, []string{"x"}) }); err == nil {
		t.Error("bad JSON sent")
	}

	reset()
	if _, err = runCmd(t, apiCmd, func() error { return apiCmd.RunE(apiCmd, []string{"DELETE", "/api/watches/1"}) }); err != nil || got[2] != "DELETE /api/watches/1 Bearer t0ken " {
		t.Errorf("delete: %v %q", err, got)
	}

	reset()
	apiArgs.SSE = true
	out, err = runCmd(t, apiCmd, func() error { return apiCmd.RunE(apiCmd, []string{"events?topics=op"}) })
	if err != nil || out != "{\"seq\":1}\n{\"seq\":2}\n" {
		t.Errorf("sse: %v %q", err, out)
	}

	reset()
	apiArgs.List = true
	out, err = runCmd(t, apiCmd, func() error { return apiCmd.RunE(apiCmd, nil) })
	if err != nil || !strings.Contains(out, "/api/v1/library/merge") || !strings.Contains(out, "write (plan: read)") {
		t.Errorf("list: %v %q", err, out)
	}
}

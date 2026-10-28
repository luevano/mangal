package remote

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/log"
)

func TestNormalizeURL(t *testing.T) {
	for raw, want := range map[string]string{
		"HTTP://NAS.lan:80/":                "http://nas.lan",
		"https://mangal.example.com:443/x/": "https://mangal.example.com/x",
		"http://[::1]:6977?a=b#c":           "http://[::1]:6977",
		"https://[::1]:443":                 "https://[::1]",
	} {
		if got, err := NormalizeURL(raw); err != nil || got != want {
			t.Errorf("%s: %q %v, want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{"", "nas.lan:6977", "ftp://nas.lan", "http://user:pass@nas.lan"} {
		if _, err := NormalizeURL(raw); !errors.Is(err, core.ErrInvalid) {
			t.Errorf("%q: %v", raw, err)
		}
	}
}

// TestTransport: the token goes only to the server's address (no redirect elsewhere), with
// one warning over plain HTTP to another machine.
func TestTransport(t *testing.T) {
	var gotAuth []string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = append(gotAuth, r.Header.Get("Authorization"))
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// the same host on another port is another address
		http.Redirect(w, r, strings.Replace(other.URL, "127.0.0.1", "localhost", 1)+r.URL.Path, http.StatusFound)
	}))
	defer srv.Close()
	c, err := New(Options{URL: srv.URL, Token: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Status(t.Context()); err == nil || !strings.Contains(err.Error(), "not followed") || strings.Contains(err.Error(), "secret") {
		t.Errorf("redirect: %v", err)
	}
	if len(gotAuth) != 0 {
		t.Errorf("the other address got %q", gotAuth)
	}

	var warnings []string
	c, err = New(Options{URL: "http://192.0.2.1:6977", Token: "secret", Warn: func(msg string) { warnings = append(warnings, msg) }})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		req, _ := http.NewRequest(http.MethodGet, "http://192.0.2.1:6977/api/v1/status", nil)
		if err := c.edit(t.Context(), req); err != nil || req.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("edit: %v %q", err, req.Header.Get("Authorization"))
		}
	}
	req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:6977/api/v1/status", nil)
	_ = c.edit(t.Context(), req)
	if len(warnings) != 1 || strings.Contains(warnings[0], "secret") {
		t.Errorf("warnings %q", warnings)
	}
}

func TestLogsFollow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/events" || r.URL.Query().Get("topics") != "log" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for i, line := range []string{`"level":"debug","message":"hidden"`, `"level":"warn","message":"one"`, `"level":"error","message":"two"`} {
			fmt.Fprintf(w, "id: %d\ndata: {\"seq\":%d,\"topic\":\"log\",\"data\":{\"time\":\"2026-10-28T10:00:00Z\",%s}}\n\n", i+1, i+1, line)
		}
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	c, err := New(Options{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	stop := errors.New("stop")
	err = c.Services().Logs.Follow(t.Context(), core.LogQuery{Level: "info"}, func(e log.Entry) error {
		got = append(got, e.Level.String()+" "+e.Message)
		if len(got) == 2 {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) || strings.Join(got, ", ") != "WARN one, ERROR two" {
		t.Errorf("got %q, %v", got, err)
	}
}

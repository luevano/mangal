package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core/remote"
)

// TestProvidersRmRefusesLocalWork: a provider that isn't a git repository stays.
func TestProvidersRmRefusesLocalWork(t *testing.T) {
	dir := t.TempDir()
	old := config.Providers.Path.Get()
	if err := config.Set(config.Providers.Path.Key, dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.Set(config.Providers.Path.Key, old) })
	if err := os.MkdirAll(filepath.Join(dir, "mine"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := providersRmCmd.RunE(providersRmCmd, []string{"mine"})
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Errorf("got %v, want a refusal", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "mine")); err != nil {
		t.Errorf("removed: %v", err)
	}
}

// providers up on a server: what each directory did, the failures as the error.
func TestProvidersUpRemote(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"providers":[{"dir":"saturno","status":"updated"},{"dir":"mine","status":"failed","error":"no remote"}],"problems":["x: bad"]}`))
	}))
	defer srv.Close()
	rc, err := remote.Open(srv.URL, "t0ken", nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := runCmd(t, providersUpCmd, func() error { return updateRemoteProviders(providersUpCmd, rc) })
	if err == nil || !strings.Contains(err.Error(), "update mine: no remote") || !strings.Contains(out, "saturno: updated") ||
		!strings.Contains(out, "warning: x: bad") || strings.Join(got, ",") != "POST /api/v1/providers/update" {
		t.Errorf("%v %q %q", err, out, got)
	}
}

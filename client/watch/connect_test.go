package watch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/adrg/xdg"
	"github.com/luevano/mangal/v5/storage"
)

func TestFind(t *testing.T) {
	old := xdg.StateHome
	xdg.StateHome = t.TempDir()
	t.Cleanup(func() { xdg.StateHome = old })
	ctx := context.Background()
	dataDB := filepath.Join(t.TempDir(), "data.db")
	file := DiscoveryPath(dataDB)
	if other := DiscoveryPath(dataDB + "2"); other == file {
		t.Fatal("same discovery file for two data dbs")
	}

	if r, _, err := Find(ctx, dataDB); r != nil || err != nil {
		t.Fatalf("no file: %v, %v", r, err)
	}

	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if auth == "Bearer wrong" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"missing or wrong token (serve.token)"}`))
			return
		}
		if r.URL.Path != "/api/status" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"server":true}`))
	}))
	defer srv.Close()
	live := Discovery{PID: os.Getpid(), Address: srv.URL, Started: time.Now(), Data: dataDB, Key: "secret"}
	if err := WriteDiscovery(file, live); err != nil {
		t.Fatal(err)
	}
	r, d, err := Find(ctx, dataDB)
	if err != nil || r == nil || d.Address != srv.URL || auth != "Bearer secret" {
		t.Fatalf("live server: %v, %+v, %v, auth %q", r, d, err, auth)
	}

	// a wrong key is an error, not a local fallback
	wrong := live
	wrong.Key = "wrong"
	if err := WriteDiscovery(file, wrong); err != nil {
		t.Fatal(err)
	}
	if r, _, err := Find(ctx, dataDB); r != nil || err == nil {
		t.Errorf("wrong key: %v, %v", r, err)
	}

	// a process that's gone: the file is removed
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	dead := live
	dead.PID = cmd.Process.Pid
	if err := WriteDiscovery(file, dead); err != nil {
		t.Fatal(err)
	}
	if r, _, err := Find(ctx, dataDB); r != nil || err != nil {
		t.Fatalf("dead server: %v, %v", r, err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("stale file kept: %v", err)
	}

	// alive but not answering: local, the file stays
	srv.Close()
	if err := WriteDiscovery(file, live); err != nil {
		t.Fatal(err)
	}
	if r, _, err := Find(ctx, dataDB); r != nil || err != nil {
		t.Fatalf("not answering: %v, %v", r, err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Errorf("file of a running process removed: %v", err)
	}
}

// TestAPIErrorMatchesRunErrors: the remote's run errors match the storage ones, as local.
func TestAPIErrorMatchesRunErrors(t *testing.T) {
	for _, local := range []error{storage.ErrRunNotFound, storage.ErrRunUnfinished, ErrNotFound, ErrBusy, ErrExists} {
		remote := &APIError{Code: ErrorCode(fmt.Errorf("x: %w", local))}
		if !errors.Is(remote, local) {
			t.Errorf("%v: code %q doesn't match it", local, remote.Code)
		}
	}
	if remote := (&APIError{Code: ErrorCode(storage.ErrRunNotFound)}); !errors.Is(remote, ErrNotFound) || errors.Is(remote, ErrBusy) {
		t.Errorf("run not found: %+v", remote)
	}
}

package serve

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/luevano/mangal/v5/client/watch"
)

// runServer runs Run in the background until the test ends, returning its address.
func runServer(t *testing.T, args Args) (address string, done <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errs := make(chan error, 1)
	go func() { errs <- Run(ctx, args, io.Discard) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-errs:
		case <-time.After(10 * time.Second):
			t.Error("serve didn't stop")
		}
	})
	file := watch.DiscoveryPath(watch.DataDB())
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if d, found, err := watch.ReadDiscovery(file); err == nil && found && d.Address != "" {
			return d.Address, errs
		}
		select {
		case err := <-errs:
			t.Fatalf("serve stopped: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatal("serve didn't start")
	return "", nil
}

func TestTokenOnLoopback(t *testing.T) {
	err := Run(context.Background(), Args{Host: "127.0.0.1", TokenOnLoopback: true}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "needs a token") {
		t.Fatalf("no token: %v", err)
	}

	address, _ := runServer(t, Args{Host: "127.0.0.1", Token: "s3cret", TokenOnLoopback: true})
	for auth, want := range map[string]int{"": http.StatusUnauthorized, "Bearer s3cret": http.StatusOK} {
		req, _ := http.NewRequest("GET", address+"/api/status", nil)
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
	if _, err := watch.NewRemote(address, "s3cret").Status(context.Background()); err != nil {
		t.Errorf("remote with the token: %v", err)
	}
}

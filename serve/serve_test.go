package serve

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
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

// TestServeLogin: every request logs in, the same machine too; mangal on the same config
// gets the discovery file's key.
func TestServeLogin(t *testing.T) {
	address, _ := runServer(t, Args{Host: "127.0.0.1"})
	d, found, err := watch.ReadDiscovery(watch.DiscoveryPath(watch.DataDB()))
	if err != nil || !found || d.Key == "" {
		t.Fatalf("discovery %+v %v %v", d, found, err)
	}
	for auth, want := range map[string]int{"": http.StatusUnauthorized, "Bearer nope": http.StatusUnauthorized, "Bearer " + d.Key: http.StatusOK} {
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
	if r, _, err := watch.Find(context.Background(), watch.DataDB()); err != nil || r == nil {
		t.Errorf("find the server: %v %v", r, err)
	}
}

// failingListener's Accept fails: the server stops at once.
type failingListener struct{ net.Listener }

func (l failingListener) Accept() (net.Conn, error) { return nil, errors.New("accept: boom") }

// TestServerFails: a failed HTTP server stops the scheduler and Run returns its error
// (systemd restarts it), it doesn't hang.
func TestServerFails(t *testing.T) {
	saved := listen
	t.Cleanup(func() { listen = saved })
	listen = func(network, address string) (net.Listener, error) {
		l, err := saved(network, address)
		return failingListener{l}, err
	}
	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), Args{Host: "127.0.0.1"}, io.Discard) }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Errorf("err = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run hangs after the server failed")
	}
}

// TestLostLock: another server taking the lock over stops this one.
func TestLostLock(t *testing.T) {
	saved := heartbeatEvery
	t.Cleanup(func() { heartbeatEvery = saved })
	heartbeatEvery = 20 * time.Millisecond
	ctx := t.Context()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, Args{Host: "127.0.0.1"}, io.Discard) }()
	runServerAddress(t)

	other, err := watch.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := other.Watches.Lock(lockName, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Unlock()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "lost") {
			t.Errorf("err = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("still serving without the lock")
	}
}

// runServerAddress waits for the discovery file of a server started elsewhere.
func runServerAddress(t *testing.T) string {
	t.Helper()
	file := watch.DiscoveryPath(watch.DataDB())
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if d, found, err := watch.ReadDiscovery(file); err == nil && found && d.Address != "" {
			return d.Address
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("serve didn't start")
	return ""
}

// syncBuffer is a bytes.Buffer for a server's output.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// TestNoUsersWarns: a server nobody can log into says how to add a user.
func TestNoUsersWarns(t *testing.T) {
	var out syncBuffer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, Args{Host: "127.0.0.1"}, &out) }()
	runServerAddress(t)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "mangal user add") {
		t.Errorf("no warning, output:\n%s", out.String())
	}
}

// TestLifecycle: a server writes its discovery file and holds the lock while it runs; once
// stopped both are gone and another one starts.
func TestLifecycle(t *testing.T) {
	file := watch.DiscoveryPath(watch.DataDB())
	for range 2 {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- Run(ctx, Args{Host: "127.0.0.1"}, io.Discard) }()
		address := runServerAddress(t)
		d, _, _ := watch.ReadDiscovery(file)
		status, err := watch.NewRemote(address, d.Key).Status(context.Background())
		if err != nil || !status.Server {
			t.Fatalf("status %+v, %v", status, err)
		}
		if err := Run(context.Background(), Args{Host: "127.0.0.1"}, io.Discard); err == nil || !strings.Contains(err.Error(), "another mangal serve") {
			t.Errorf("second server: %v", err)
		}
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("didn't stop")
		}
		if _, found, err := watch.ReadDiscovery(file); err != nil || found {
			t.Errorf("discovery file left: %v, %v", found, err)
		}
	}
}

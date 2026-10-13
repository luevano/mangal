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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
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

func TestShortTokenWarns(t *testing.T) {
	for token, warns := range map[string]bool{"s3cret": true, "0123456789abcdef": false} {
		var out syncBuffer
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- Run(ctx, Args{Host: "127.0.0.1", Token: token, TokenOnLoopback: true}, &out) }()
		runServerAddress(t)
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(out.String(), "token is short"); got != warns {
			t.Errorf("%q: warns %v, output:\n%s", token, got, out.String())
		}
	}
}

package serve

import (
	"bufio"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/luevano/mangal/v5/storage"
)

// openStream opens /api/v1/events as the authorization on s, past its first line.
func openStream(t *testing.T, s *testServer, authorization string) (*http.Response, *bufio.Reader) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), "GET", s.URL+"/api/v1/events?topics=job", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", authorization)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		return res, nil
	}
	br := bufio.NewReader(res.Body)
	if line, err := br.ReadString('\n'); err != nil || !strings.HasPrefix(line, ": mangal serve events") {
		t.Fatalf("first line %q %v", line, err)
	}
	return res, br
}

// ended is true once the stream's body ends within a few seconds.
func ended(br *bufio.Reader) bool {
	done := make(chan struct{})
	go func() {
		for {
			if _, err := br.ReadString('\n'); err != nil {
				close(done)
				return
			}
		}
	}()
	select {
	case <-done:
		return true
	case <-time.After(5 * time.Second):
		return false
	}
}

// TestStreamEndsWithLogin: removing a user (another process: the watcher's auth change)
// ends their open stream; another user's stays.
func TestStreamEndsWithLogin(t *testing.T) {
	s := newTestServer(t, "")
	addUser(t, s, "kid", "a long password", ScopeRead)
	addUser(t, s, "teen", "a long password", ScopeRead)
	_, kid := openStream(t, s, basic("kid", "a long password"))
	_, teen := openStream(t, s, basic("teen", "a long password"))
	if err := (storage.Users{DB: s.svc.Watches.DB}).Remove("kid"); err != nil {
		t.Fatal(err)
	}
	s.api.dbChanged([]string{storage.RevAuth})
	if !ended(kid) {
		t.Error("kid's stream still open")
	}
	s.api.streams.mu.Lock()
	n := len(s.api.streams.open)
	s.api.streams.mu.Unlock()
	if n != 1 || teen == nil {
		t.Errorf("%d streams open, want teen's", n)
	}
}

// TestStreamLimits: a stream past its life ends (the client connects again), and an
// identity can't open more than streamsPerIdentity.
func TestStreamLimits(t *testing.T) {
	life, most := streamLife, streamsPerIdentity
	streamLife, streamsPerIdentity = 300*time.Millisecond, 2
	t.Cleanup(func() { streamLife, streamsPerIdentity = life, most })
	s := newTestServer(t, "")
	addUser(t, s, "kid", "a long password", ScopeRead)
	kid := basic("kid", "a long password")
	_, first := openStream(t, s, kid)
	openStream(t, s, kid)
	if res, _ := openStream(t, s, kid); res.StatusCode != http.StatusTooManyRequests {
		t.Errorf("third stream: %d", res.StatusCode)
	}
	if !ended(first) {
		t.Error("the stream outlived its life")
	}
}

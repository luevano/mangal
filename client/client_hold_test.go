package client

import (
	"context"
	"sync"
	"testing"

	"github.com/luevano/libmangal"
)

func resetLingering(t *testing.T) {
	t.Cleanup(func() {
		clientsMu.Lock()
		lingering = map[*entry]struct{}{}
		clientsMu.Unlock()
	})
}

// trackLoader keeps the providers it loads, in order.
type trackLoader struct {
	fakeLoader
	mu    *sync.Mutex
	loads *[]*fakeProvider
}

func newTrackLoader(id string) trackLoader {
	return trackLoader{fakeLoader: fakeLoader{id}, mu: &sync.Mutex{}, loads: &[]*fakeProvider{}}
}

func (l trackLoader) Load(context.Context) (libmangal.Provider, error) {
	p := &fakeProvider{info: l.Info()}
	l.mu.Lock()
	*l.loads = append(*l.loads, p)
	l.mu.Unlock()
	return p, nil
}

func (l trackLoader) last() *fakeProvider {
	l.mu.Lock()
	defer l.mu.Unlock()
	return (*l.loads)[len(*l.loads)-1]
}

func (l trackLoader) all() []*fakeProvider {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]*fakeProvider(nil), *l.loads...)
}

// TestCloseAllKeepsHeldClients: the TUI's CloseAll doesn't close the client of a check
// running in the background, its last release does.
func TestCloseAllKeepsHeldClients(t *testing.T) {
	created := fakeRegistry(t, nil)
	resetLingering(t)
	ctx := context.Background()
	loader := newTrackLoader("a")

	held, release, err := Hold(ctx, loader)
	if err != nil {
		t.Fatal(err)
	}
	p := loader.last()
	other := newTrackLoader("b")
	if _, err := Acquire(ctx, other); err != nil {
		t.Fatal(err)
	}
	idle := other.last()

	if err := CloseAll(); err != nil {
		t.Fatal(err)
	}
	if p.closed.Load() != 0 {
		t.Fatal("closed a held client")
	}
	if idle.closed.Load() != 1 {
		t.Error("idle client not closed")
	}
	// the next one is a new client
	if c, err := Acquire(ctx, loader); err != nil || c == held || created.Load() != 3 {
		t.Fatalf("acquired %p (held %p), created %d, %v", c, held, created.Load(), err)
	}
	release()
	release()
	if p.closed.Load() != 1 {
		t.Errorf("closed %d times after the release", p.closed.Load())
	}
}

func TestReleaseIfAuthChangedKeepsHeldClients(t *testing.T) {
	fakeRegistry(t, nil)
	resetLingering(t)
	ctx := context.Background()
	loader := newTrackLoader("a")
	_, release, err := Hold(ctx, loader)
	if err != nil {
		t.Fatal(err)
	}
	p := loader.last()
	authFingerprint = func() (string, error) { return "v2", nil }
	if released, err := ReleaseIfAuthChanged("a"); err != nil || !released {
		t.Fatalf("not released: %v %v", released, err)
	}
	if p.closed.Load() != 0 {
		t.Fatal("closed a held client")
	}
	release()
	if p.closed.Load() != 1 {
		t.Error("not closed after the release")
	}
}

func TestShutdownClosesHeldClients(t *testing.T) {
	fakeRegistry(t, nil)
	resetLingering(t)
	loader := newTrackLoader("a")
	_, release, err := Hold(context.Background(), loader)
	if err != nil {
		t.Fatal(err)
	}
	p := loader.last()
	if err := CloseAll(); err != nil {
		t.Fatal(err)
	}
	if err := Shutdown(); err != nil {
		t.Fatal(err)
	}
	release()
	if p.closed.Load() != 1 {
		t.Errorf("closed %d times", p.closed.Load())
	}
}

func TestHoldConcurrentCloseAll(t *testing.T) {
	fakeRegistry(t, nil)
	resetLingering(t)
	ctx := context.Background()
	loader := newTrackLoader("a")
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 50 {
				_, release, err := Hold(ctx, loader)
				if err != nil {
					t.Error(err)
					return
				}
				release()
			}
		})
	}
	wg.Go(func() {
		for range 50 {
			_ = CloseAll()
		}
	})
	wg.Wait()
	if err := Shutdown(); err != nil {
		t.Fatal(err)
	}
	for i, p := range loader.all() {
		if n := p.closed.Load(); n != 1 {
			t.Errorf("client %d closed %d times", i, n)
		}
	}
}

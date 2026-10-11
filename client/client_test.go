package client

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/logger"
)

type fakeLoader struct{ id string }

func (l fakeLoader) String() string { return l.id }
func (l fakeLoader) Info() libmangal.ProviderInfo {
	return libmangal.ProviderInfo{ID: l.id, Name: l.id, Version: "0.1.0"}
}

func (l fakeLoader) Load(context.Context) (libmangal.Provider, error) {
	return &fakeProvider{info: l.Info()}, nil
}

type fakeProvider struct {
	libmangal.Provider
	info   libmangal.ProviderInfo
	closed atomic.Int32
}

func (p *fakeProvider) Info() libmangal.ProviderInfo { return p.info }
func (p *fakeProvider) String() string               { return p.info.Name }
func (p *fakeProvider) SetLogger(*logger.Logger)     {}
func (p *fakeProvider) Close() error {
	p.closed.Add(1)
	return nil
}

// fakeRegistry empties the registry and creates clients without the config and the dbs,
// counting them.
func fakeRegistry(t *testing.T, before func(ctx context.Context) error) *atomic.Int32 {
	t.Helper()
	var created atomic.Int32
	clientsMu.Lock()
	clients = map[string]*entry{}
	clientsMu.Unlock()
	create = func(ctx context.Context, loader libmangal.ProviderLoader) (*libmangal.Client, error) {
		if before != nil {
			if err := before(ctx); err != nil {
				return nil, err
			}
		}
		created.Add(1)
		return libmangal.NewClientContext(ctx, loader, libmangal.DefaultClientOptions())
	}
	t.Cleanup(func() {
		create = newClient
		clientsMu.Lock()
		clients = map[string]*entry{}
		clientsMu.Unlock()
	})
	return &created
}

func TestAcquireSameClient(t *testing.T) {
	created := fakeRegistry(t, nil)
	ctx := context.Background()
	loader := fakeLoader{"a"}

	var wg sync.WaitGroup
	got := make([]*libmangal.Client, 20)
	for i := range got {
		wg.Go(func() {
			c, err := Acquire(ctx, loader)
			if err != nil {
				t.Error(err)
			}
			got[i] = c
		})
	}
	wg.Wait()
	for _, c := range got {
		if c == nil || c != got[0] {
			t.Fatal("Acquire returned different clients for the same loader")
		}
	}
	if n := created.Load(); n != 1 {
		t.Errorf("created %d clients, want 1", n)
	}
	if Get(loader) != got[0] {
		t.Error("Get doesn't return the acquired client")
	}

	other, err := Acquire(ctx, fakeLoader{"b"})
	if err != nil || other == got[0] {
		t.Errorf("another loader got %v, %v", other, err)
	}
}

func TestReleaseAndCloseAll(t *testing.T) {
	created := fakeRegistry(t, nil)
	ctx := context.Background()
	a, _ := Acquire(ctx, fakeLoader{"a"})
	_, _ = Acquire(ctx, fakeLoader{"b"})

	if err := Release(a); err != nil {
		t.Fatal(err)
	}
	if Get(fakeLoader{"a"}) != nil {
		t.Error("released client still registered")
	}
	a2, _ := Acquire(ctx, fakeLoader{"a"})
	if a2 == a || created.Load() != 3 {
		t.Error("Acquire after Release didn't create a new client")
	}

	if err := CloseAll(); err != nil {
		t.Fatal(err)
	}
	if Get(fakeLoader{"a"}) != nil || Get(fakeLoader{"b"}) != nil {
		t.Error("clients still registered after CloseAll")
	}
	// closed once each, not again by a later CloseAll
	if err := CloseAll(); err != nil {
		t.Fatal(err)
	}
}

func TestAcquireCreatorCancelled(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	fakeRegistry(t, func(ctx context.Context) error {
		first := false
		once.Do(func() { first = true })
		if first {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	})
	loader := fakeLoader{"a"}

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error)
	go func() {
		_, err := Acquire(ctx, loader)
		errc <- err
	}()
	<-started

	// waits on the first creation, then creates it itself
	res := make(chan *libmangal.Client)
	go func() {
		c, err := Acquire(context.Background(), loader)
		if err != nil {
			t.Error(err)
		}
		res <- c
	}()
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled Acquire = %v", err)
	}
	if c := <-res; c == nil {
		t.Error("waiting Acquire got no client")
	}
}

func TestAcquireError(t *testing.T) {
	fail := errors.New("load failed")
	calls := 0
	fakeRegistry(t, func(context.Context) error {
		calls++
		if calls == 1 {
			return fail
		}
		return nil
	})
	if _, err := Acquire(context.Background(), fakeLoader{"a"}); !errors.Is(err, fail) {
		t.Fatalf("err = %v", err)
	}
	// a failed creation isn't kept
	if _, err := Acquire(context.Background(), fakeLoader{"a"}); err != nil {
		t.Fatal(err)
	}
}

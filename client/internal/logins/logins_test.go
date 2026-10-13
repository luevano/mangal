package logins

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWaitsAreCancellable(t *testing.T) {
	a := New(Service{Name: "test", Check: func(context.Context) error { return nil }, Drop: func() error { return nil }})
	if err := a.lock(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer a.unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	for name, f := range map[string]func(context.Context) error{
		"ensure": a.Ensure, "check": a.CheckLogin, "reset": a.Reset, "logout": a.Logout,
	} {
		if err := f(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("%s while another login runs: %v", name, err)
		}
	}
}

func TestEnsureOncePerProcess(t *testing.T) {
	checks, drops := 0, 0
	a := New(Service{Name: "test", Check: func(context.Context) error { checks++; return nil }, Drop: func() error { drops++; return nil }})
	for range 2 {
		if err := a.Ensure(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Reset(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if checks != 2 || drops != 1 {
		t.Errorf("checks %d, drops %d", checks, drops)
	}
}

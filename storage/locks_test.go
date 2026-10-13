package storage

import (
	"errors"
	"testing"
	"time"
)

func TestLock(t *testing.T) {
	data := openData(t)
	watches := Watches{DB: data}
	lock, err := watches.Lock("check", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var lockErr *LockError
	if _, err := watches.Lock("check", time.Minute); !errors.As(err, &lockErr) || !errors.Is(err, ErrLocked) || lockErr.Holder.PID == 0 {
		t.Fatalf("locked twice: %v", err)
	}
	if other, err := watches.LockAs("other", "download", time.Minute); err != nil {
		t.Errorf("another lock: %v", err)
	} else {
		if _, err := watches.Lock("other", time.Minute); !errors.As(err, &lockErr) || lockErr.Holder.Purpose != "download" {
			t.Errorf("purpose: %v", err)
		}
		_ = other.Unlock()
	}
	if err := lock.Heartbeat(); err != nil {
		t.Error(err)
	}

	// a stale lock is taken over, the old holder lost it
	if _, err := data.Exec(`UPDATE locks SET heartbeat = heartbeat - 3600`); err != nil {
		t.Fatal(err)
	}
	taken, err := watches.Lock("check", time.Minute)
	if err != nil {
		t.Fatalf("stale lock not taken: %v", err)
	}
	if err := lock.Heartbeat(); !errors.Is(err, ErrLockLost) {
		t.Errorf("the old holder still has it: %v", err)
	}
	_ = lock.Unlock() // not its lock anymore, no-op
	if _, err := watches.Lock("check", time.Minute); !errors.Is(err, ErrLocked) {
		t.Errorf("unlocked by the old holder: %v", err)
	}
	if err := taken.Unlock(); err != nil {
		t.Fatal(err)
	}
	if l, err := watches.Lock("check", time.Minute); err != nil {
		t.Errorf("after unlock: %v", err)
	} else {
		_ = l.Unlock()
	}

	// a holder of this host that doesn't run anymore loses it at once
	dead, err := watches.Lock("check", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := data.Exec(`UPDATE locks SET pid = 0`); err != nil {
		t.Fatal(err)
	}
	if l, err := watches.Lock("check", time.Minute); err != nil {
		t.Errorf("lock of a dead process not taken: %v", err)
	} else {
		_ = l.Unlock()
	}
	_ = dead.Unlock()
}

func TestLockHolders(t *testing.T) {
	data := openData(t)
	watches := Watches{DB: data}
	for _, name := range []string{"watch-check:b", "watch-check:a", "serve", "watch-check:dead"} {
		if _, err := watches.Lock(name, time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := data.Exec(`UPDATE locks SET pid = 0 WHERE name = 'watch-check:dead'`); err != nil {
		t.Fatal(err)
	}
	held, err := watches.Holders("watch-check:", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(held) != 2 || held[0].Name != "watch-check:a" || held[1].Name != "watch-check:b" || held[0].PID == 0 {
		t.Errorf("holders %+v", held)
	}
}

func TestLockKeepAlive(t *testing.T) {
	watches := Watches{DB: openData(t)}
	lock, err := watches.Lock("beat", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := watches.DB.Exec(`UPDATE locks SET heartbeat = 0`); err != nil {
		t.Fatal(err)
	}
	lost := make(chan error, 1)
	stop := lock.KeepAlive(5*time.Millisecond, func(err error) { lost <- err }, nil)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, held, err := watches.Holder("beat", time.Minute); err != nil || held {
			if err != nil {
				t.Fatal(err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no heartbeat")
		}
		time.Sleep(5 * time.Millisecond)
	}

	if _, err := watches.DB.Exec(`UPDATE locks SET token = 'other'`); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-lost:
		if !errors.Is(err, ErrLockLost) {
			t.Errorf("lost: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lost lock not seen")
	}
	stop()
	stop()
}

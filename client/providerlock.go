package client

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
)

// ProviderLockPrefix starts the data db lock of each provider: provider:<id>. It's held by
// whatever downloads from the provider (a watch check's run of it, a download), so only one
// at a time downloads from it, across the processes sharing the data db.
const ProviderLockPrefix = "provider:"

// ProviderLockName is the data db lock of the provider.
func ProviderLockName(provider string) string {
	return ProviderLockPrefix + provider
}

// ProviderLockStale is how long a provider's lock lives without a heartbeat.
const ProviderLockStale = 10 * time.Minute

// What a provider's lock is held for (storage.LockHolder.Purpose).
const (
	LockForDownload = "download"
	LockForWatch    = "watch"
)

// swapped in tests
var (
	providerLockPoll  = 2 * time.Second
	providerLockBeat  = ProviderLockStale / 5
	providerLockStore = cache.Watches
)

// ProviderBusy is a provider whose lock someone else holds, a download waits for it.
type ProviderBusy struct {
	Provider string
	Holder   storage.LockHolder
	// Watch is true when the holder is a watch check.
	Watch bool
}

// Message is "<provider> is busy downloading in another mangal (pid N), waiting…" (or
// being checked by a watch).
func (b ProviderBusy) Message() string {
	switch {
	case b.Watch:
		return fmt.Sprintf("%s is being checked by a watch (pid %d), waiting…", b.Provider, b.Holder.PID)
	case b.Holder.PID == os.Getpid():
		return fmt.Sprintf("%s is busy with another download, waiting…", b.Provider)
	}
	return fmt.Sprintf("%s is busy downloading in another mangal (pid %d), waiting…", b.Provider, b.Holder.PID)
}

type heldLockKey struct{ name string }

// HoldingProvider is ctx saying its holder has the provider's lock (taken elsewhere, e.g.
// by a watch check), so downloads with it don't take it again.
func HoldingProvider(ctx context.Context, provider string) context.Context {
	return context.WithValue(ctx, heldLockKey{ProviderLockName(provider)}, true)
}

// LockProvider takes the provider's lock in the store for purpose (LockForDownload,
// LockForWatch), waiting while someone else holds it (onWait, which may be nil, is told
// once). The returned ctx carries the lock (HoldingProvider) and is cancelled when the
// lock is lost to another mangal (cause storage.ErrLockLost). unlock releases it (nothing
// when ctx already had it). A cancelled ctx stops the wait.
func LockProvider(ctx context.Context, store storage.Watches, provider, purpose string, onWait func(ProviderBusy)) (_ context.Context, unlock func(), _ error) {
	name := ProviderLockName(provider)
	if ctx.Value(heldLockKey{name}) != nil {
		return ctx, func() {}, nil
	}
	waited := false
	for {
		lock, err := store.LockAs(name, purpose, ProviderLockStale)
		if err == nil {
			held, lost := context.WithCancelCause(HoldingProvider(ctx, provider))
			stop := keepAlive(lock, lost)
			return held, func() {
				stop()
				lost(nil)
			}, nil
		}
		var locked *storage.LockError
		if !errors.As(err, &locked) {
			return ctx, nil, err
		}
		if !waited {
			waited = true
			log.Info("provider %s is locked by pid %d on %s (%s), waiting", provider, locked.Holder.PID, locked.Holder.Host, locked.Holder.Purpose)
			if onWait != nil {
				onWait(ProviderBusy{Provider: provider, Holder: locked.Holder, Watch: locked.Holder.Purpose == LockForWatch})
			}
		}
		timer := time.NewTimer(providerLockPoll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx, nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// WaitProviderFree waits while someone holds the provider's lock (polling like
// LockProvider; onWait, which may be nil, is told once), without taking it. A cancelled
// ctx stops the wait.
func WaitProviderFree(ctx context.Context, store storage.Watches, provider string, onWait func(ProviderBusy)) error {
	waited := false
	for {
		holder, held, err := store.Holder(ProviderLockName(provider), ProviderLockStale)
		if err != nil || !held {
			return err
		}
		if !waited {
			waited = true
			if onWait != nil {
				onWait(ProviderBusy{Provider: provider, Holder: holder, Watch: holder.Purpose == LockForWatch})
			}
		}
		timer := time.NewTimer(providerLockPoll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// keepAlive beats the lock until the returned unlock. A lost lock calls lost.
func keepAlive(lock *storage.Lock, lost func(error)) func() {
	stop := lock.KeepAlive(providerLockBeat, func(err error) {
		log.Warn("%s: another mangal took it over", err)
		lost(err)
	}, func(err error) { log.Warn("%s", err) })
	return func() {
		stop()
		if err := lock.Unlock(); err != nil {
			log.Warn("%s", err)
		}
	}
}

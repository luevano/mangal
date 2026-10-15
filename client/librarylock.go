package client

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/storage"
)

// LibraryLockName is the data db lock of the library, held by every library operation
// (Operation, an undo, emptying the trash) so two mangal processes never change the
// library at once.
const LibraryLockName = "library"

// LibraryLockStale is how long the library's lock lives without a heartbeat.
const LibraryLockStale = ProviderLockStale

// ErrLibraryBusy is the library's lock held by someone else, for a caller that doesn't wait.
var ErrLibraryBusy = errors.New("the library is busy")

// swapped in tests
var (
	libraryLockPoll = 2 * time.Second
	libraryLockBeat = LibraryLockStale / 5
)

// LibraryBusy is the library's lock held by someone else: what for (the operation's
// kind) and who.
type LibraryBusy struct {
	Holder storage.LockHolder
}

// Message is "the library is busy (merge) in another mangal (pid N), waiting…".
func (b LibraryBusy) Message() string {
	what := ""
	if b.Holder.Purpose != "" {
		what = " (" + b.Holder.Purpose + ")"
	}
	if b.Holder.PID == os.Getpid() {
		return fmt.Sprintf("the library is busy with another change%s, waiting…", what)
	}
	return fmt.Sprintf("the library is busy%s in another mangal (pid %d), waiting…", what, b.Holder.PID)
}

// LibraryBusyError is the library's lock held by someone else, it matches ErrLibraryBusy.
type LibraryBusyError struct {
	Holder storage.LockHolder
}

func (e *LibraryBusyError) Error() string {
	what := "another change"
	if e.Holder.Purpose != "" {
		what = "a " + e.Holder.Purpose
	}
	return fmt.Sprintf("the library is busy: %s in mangal pid %d on %s since %s", what, e.Holder.PID, e.Holder.Host, e.Holder.Acquired.Format(time.DateTime))
}

func (e *LibraryBusyError) Is(target error) bool { return target == ErrLibraryBusy }

type heldLibraryKey struct{}

// HoldingLibrary is true when ctx carries the library's lock (LockLibrary).
func HoldingLibrary(ctx context.Context) bool {
	return ctx.Value(heldLibraryKey{}) != nil
}

// LockLibrary takes the library's lock in the store for purpose (the operation's kind).
// While someone else holds it, it waits (onWait, which may be nil, is told once) or, when
// wait is false, returns a *LibraryBusyError. The returned ctx carries the lock (a
// LockLibrary with it takes nothing) and is cancelled when the lock is lost to another
// mangal (cause storage.ErrLockLost). unlock releases it. A cancelled ctx stops the wait.
func LockLibrary(ctx context.Context, store storage.Watches, purpose string, wait bool, onWait func(LibraryBusy)) (_ context.Context, unlock func(), _ error) {
	if HoldingLibrary(ctx) {
		return ctx, func() {}, nil
	}
	waited := false
	for {
		lock, err := store.LockAs(LibraryLockName, purpose, LibraryLockStale)
		if err == nil {
			held, lost := context.WithCancelCause(context.WithValue(ctx, heldLibraryKey{}, true))
			stop := lock.KeepAlive(libraryLockBeat, func(err error) {
				log.Warn("%s: another mangal took it over", err)
				lost(err)
			}, func(err error) { log.Warn("%s", err) })
			return held, func() {
				stop()
				if err := lock.Unlock(); err != nil {
					log.Warn("%s", err)
				}
				lost(nil)
			}, nil
		}
		var locked *storage.LockError
		if !errors.As(err, &locked) {
			return ctx, nil, err
		}
		if !wait {
			return ctx, nil, &LibraryBusyError{Holder: locked.Holder}
		}
		if !waited {
			waited = true
			log.Info("the library is locked by pid %d on %s (%s), waiting", locked.Holder.PID, locked.Holder.Host, locked.Holder.Purpose)
			if onWait != nil {
				onWait(LibraryBusy{Holder: locked.Holder})
			}
		}
		timer := time.NewTimer(libraryLockPoll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx, nil, ctx.Err()
		case <-timer.C:
		}
	}
}

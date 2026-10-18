package client

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync/atomic"
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

// ChapterLockPrefix starts the data db lock a download holds while it writes one chapter
// (chapter:<provider>:<pid>:<n>): a library operation waits for them (Ops.Lock).
const ChapterLockPrefix = "chapter:"

var chapterSeq atomic.Int64

// LockChapter marks a chapter of the provider as being written, after waiting while a
// library operation runs (onWait, which may be nil, is told once). done unmarks it. With
// ctx holding the library's lock nothing is marked. A cancelled ctx stops the wait.
func LockChapter(ctx context.Context, store storage.Watches, provider string, onWait func(LibraryBusy)) (done func(), _ error) {
	if HoldingLibrary(ctx) {
		return func() {}, nil
	}
	waited := false
	once := func(b LibraryBusy) {
		if !waited {
			waited = true
			if onWait != nil {
				onWait(b)
			}
		}
	}
	for {
		if err := WaitLibraryFree(ctx, store, once); err != nil {
			return nil, err
		}
		name := fmt.Sprintf("%s%s:%d:%d", ChapterLockPrefix, provider, os.Getpid(), chapterSeq.Add(1))
		lock, err := store.LockAs(name, LockForDownload, ProviderLockStale)
		if err != nil {
			return nil, err
		}
		done := keepAlive(lock, func(error) {})
		// the library's lock taken since the wait: the operation waits for this marker,
		// so let it go first
		if _, held, err := libraryHolder(store); err != nil || held {
			done()
			if err != nil {
				return nil, err
			}
			continue
		}
		return done, nil
	}
}

// libraryHolder is who holds the library's lock, false when nobody does.
func libraryHolder(store storage.Watches) (storage.LockHolder, bool, error) {
	holders, err := store.Holders(LibraryLockName, LibraryLockStale)
	if err != nil {
		return storage.LockHolder{}, false, err
	}
	i := slices.IndexFunc(holders, func(h storage.LockHolder) bool { return h.Name == LibraryLockName })
	if i < 0 {
		return storage.LockHolder{}, false, nil
	}
	return holders[i], true, nil
}

// WaitLibraryFree waits while someone else holds the library's lock (polling like
// LockLibrary; onWait, which may be nil, is told once), without taking it. With ctx
// holding it, it doesn't wait. A cancelled ctx stops the wait.
func WaitLibraryFree(ctx context.Context, store storage.Watches, onWait func(LibraryBusy)) error {
	if HoldingLibrary(ctx) {
		return nil
	}
	waited := false
	for {
		holder, held, err := libraryHolder(store)
		if err != nil || !held {
			return err
		}
		if !waited {
			waited = true
			if onWait != nil {
				onWait(LibraryBusy{Holder: holder})
			}
		}
		timer := time.NewTimer(libraryLockPoll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

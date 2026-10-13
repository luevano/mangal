package storage

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/luevano/mangal/v5/util/proc"
)

// ErrLockLost is a lock taken over by another holder (its heartbeat was stale).
var ErrLockLost = errors.New("lock lost")

// ErrLocked is a lock held by another process (or another holder in this one).
var ErrLocked = errors.New("locked")

// migration 7: what a lock is held for (e.g. a manga's: a watch check or a download).
const lockPurpose = `ALTER TABLE locks ADD COLUMN purpose TEXT NOT NULL DEFAULT ''`

// Lock is a lock held in the data database, so two processes don't do the same work at
// once (e.g. check the watches). It's kept alive with Heartbeat; a lock whose heartbeat is
// older than its staleness is taken over.
type Lock struct {
	Name string
	// Acquired is when it was taken.
	Acquired time.Time
	token    string
	db       *DB
}

// LockHolder is who holds a lock.
type LockHolder struct {
	// Name is the lock's, set by Holders.
	Name string
	// Purpose is what it's held for (LockAs), may be empty.
	Purpose   string
	PID       int
	Host      string
	Acquired  time.Time
	Heartbeat time.Time
}

// LockError is a lock held by someone else, it matches ErrLocked.
type LockError struct {
	Name   string
	Holder LockHolder
}

func (e *LockError) Error() string {
	return fmt.Sprintf("%s is locked by pid %d on %s since %s", e.Name, e.Holder.PID, e.Holder.Host, e.Holder.Acquired.Format(time.DateTime))
}

func (e *LockError) Is(target error) bool { return target == ErrLocked }

// Lock takes the named lock, a *LockError if someone else holds it and its heartbeat is
// newer than stale. A lock held by a process of this host that doesn't run anymore is
// taken over right away.
func (s Watches) Lock(name string, stale time.Duration) (*Lock, error) {
	return s.LockAs(name, "", stale)
}

// LockAs is Lock saying what the lock is held for (LockHolder.Purpose).
func (s Watches) LockAs(name, purpose string, stale time.Duration) (*Lock, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(b)
	host, _ := os.Hostname()
	now := time.Now()
	err := s.DB.InTx(func(tx *sql.Tx) error {
		var h LockHolder
		var acquired, heartbeat int64
		err := tx.QueryRow(`SELECT purpose, pid, host, acquired, heartbeat FROM locks WHERE name = ?`, name).Scan(&h.Purpose, &h.PID, &h.Host, &acquired, &heartbeat)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return err
		case h.Host == host && !proc.Alive(h.PID):
		case now.Sub(time.Unix(heartbeat, 0)) < stale:
			h.Acquired, h.Heartbeat = time.Unix(acquired, 0), time.Unix(heartbeat, 0)
			return &LockError{Name: name, Holder: h}
		}
		_, err = tx.Exec(`INSERT INTO locks (name, token, purpose, pid, host, acquired, heartbeat) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (name) DO UPDATE SET token = excluded.token, purpose = excluded.purpose, pid = excluded.pid, host = excluded.host,
	acquired = excluded.acquired, heartbeat = excluded.heartbeat`, name, token, purpose, os.Getpid(), host, now.Unix(), now.Unix())
		return err
	})
	if err != nil {
		if errors.Is(err, ErrLocked) {
			return nil, err
		}
		return nil, fmt.Errorf("lock %s: %w", name, err)
	}
	return &Lock{Name: name, Acquired: time.Unix(now.Unix(), 0), token: token, db: s.DB}, nil
}

// Holder is who holds the named lock, false when nobody does (or its heartbeat is older
// than stale).
func (s Watches) Holder(name string, stale time.Duration) (LockHolder, bool, error) {
	var h LockHolder
	var acquired, heartbeat int64
	err := s.DB.QueryRow(`SELECT purpose, pid, host, acquired, heartbeat FROM locks WHERE name = ?`, name).Scan(&h.Purpose, &h.PID, &h.Host, &acquired, &heartbeat)
	if errors.Is(err, sql.ErrNoRows) {
		return h, false, nil
	}
	if err != nil {
		return h, false, fmt.Errorf("lock %s: %w", name, err)
	}
	h.Acquired, h.Heartbeat = time.Unix(acquired, 0), time.Unix(heartbeat, 0)
	return h, time.Since(h.Heartbeat) < stale, nil
}

// Holders are the held locks whose name starts with prefix, by name: heartbeat newer than
// stale and, on this host, a holder that runs.
func (s Watches) Holders(prefix string, stale time.Duration) ([]LockHolder, error) {
	rows, err := s.DB.Query(`SELECT name, purpose, pid, host, acquired, heartbeat FROM locks WHERE substr(name, 1, length(?1)) = ?1 ORDER BY name`, prefix)
	if err != nil {
		return nil, fmt.Errorf("locks %s: %w", prefix, err)
	}
	defer rows.Close()
	host, _ := os.Hostname()
	var held []LockHolder
	for rows.Next() {
		var h LockHolder
		var acquired, heartbeat int64
		if err := rows.Scan(&h.Name, &h.Purpose, &h.PID, &h.Host, &acquired, &heartbeat); err != nil {
			return nil, fmt.Errorf("locks %s: %w", prefix, err)
		}
		h.Acquired, h.Heartbeat = time.Unix(acquired, 0), time.Unix(heartbeat, 0)
		if time.Since(h.Heartbeat) >= stale || (h.Host == host && !proc.Alive(h.PID)) {
			continue
		}
		held = append(held, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("locks %s: %w", prefix, err)
	}
	return held, nil
}

// Heartbeat keeps the lock alive, an error if it was lost (taken over as stale).
func (l *Lock) Heartbeat() error {
	res, err := l.db.Exec(`UPDATE locks SET heartbeat = ? WHERE name = ? AND token = ?`, time.Now().Unix(), l.Name, l.token)
	if err != nil {
		return fmt.Errorf("lock %s heartbeat: %w", l.Name, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("lock %s: %w", l.Name, ErrLockLost)
	}
	return nil
}

// Unlock releases the lock, if still held.
func (l *Lock) Unlock() error {
	if _, err := l.db.Exec(`DELETE FROM locks WHERE name = ? AND token = ?`, l.Name, l.token); err != nil {
		return fmt.Errorf("unlock %s: %w", l.Name, err)
	}
	return nil
}

// KeepAlive beats the lock every interval until the returned stop (which doesn't unlock
// it). A lock taken over by someone else (ErrLockLost) stops beating and calls lost, other
// heartbeat errors go to warn; either may be nil.
func (l *Lock) KeepAlive(every time.Duration, lost, warn func(error)) (stop func()) {
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				err := l.Heartbeat()
				switch {
				case err == nil:
				case errors.Is(err, ErrLockLost):
					if lost != nil {
						lost(err)
					}
					return
				case warn != nil:
					warn(err)
				}
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() { close(done) })
		<-stopped
	}
}

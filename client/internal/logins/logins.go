// Package logins is what the AniList, Kitsu and MyAnimeList clients share: the saved users,
// the logout that sticks across runs and the login of this process (once, serialized).
package logins

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/luevano/libmangal/metadata"
	mlog "github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/util/cache"
)

// ErrUserNotFound is a user of the history without a saved login (it's removed from the history).
var ErrUserNotFound = errors.New("saved login not found")

// ExpiredError is a saved login that expired, or the service rejected.
type ExpiredError struct {
	User string
	On   time.Time
}

func (e *ExpiredError) Error() string {
	return fmt.Sprintf("the login of %q expired on %s, log in again", e.User, e.On.Format(time.DateOnly))
}

// Service is how a metadata service's logins are saved and checked.
type Service struct {
	// Name is the service's, in errors and logs (e.g. "anilist").
	Name string
	// History is its auth history key (the saved users).
	History string
	// LoggedOut and SetLoggedOut read and save the logout that sticks across runs.
	LoggedOut    func() (bool, error)
	SetLoggedOut func(bool) error
	// DeleteToken deletes the saved login of the user.
	DeleteToken func(user string) error
	// Check logs in with the last user's saved login, unless logged in already or it
	// expired. It runs holding the login lock.
	Check func(ctx context.Context) error
	// Drop logs this process out (no request), if it's logged in.
	Drop func() error
}

// Account is a service's saved logins and the login of this process.
type Account struct {
	Service

	// sem serializes the logins, a channel so waiting can be cancelled
	sem   chan struct{}
	tried bool

	errMu sync.Mutex
	err   error
}

// New is the account of the service.
func New(s Service) *Account {
	return &Account{Service: s, sem: make(chan struct{}, 1)}
}

func (a *Account) lock(ctx context.Context) error {
	select {
	case a.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *Account) unlock() { <-a.sem }

// Ensure logs in with the saved user once per process (see Check). A cancelled attempt is
// tried again by the next call.
func (a *Account) Ensure(ctx context.Context) error {
	if err := a.lock(ctx); err != nil {
		return err
	}
	defer a.unlock()
	if a.tried {
		return nil
	}
	err := a.Check(ctx)
	if ctx.Err() == nil {
		a.tried = true
	}
	if err != nil {
		mlog.Warn("%s login: %s", a.Name, err)
	}
	return err
}

// CheckLogin runs Check holding the login lock.
func (a *Account) CheckLogin(ctx context.Context) error {
	if err := a.lock(ctx); err != nil {
		return err
	}
	defer a.unlock()
	return a.Check(ctx)
}

// Reset logs this process out and makes the next Ensure log in again with the saved login
// (it changed, e.g. from a TUI while `mangal serve` runs).
func (a *Account) Reset(ctx context.Context) error {
	if err := a.lock(ctx); err != nil {
		return err
	}
	defer a.unlock()
	a.tried = false
	return a.Drop()
}

// Login runs login holding the login lock (so Ensure doesn't race it), and is the user it
// logged in.
func (a *Account) Login(ctx context.Context, login func() (metadata.User, error)) (metadata.User, error) {
	if err := a.lock(ctx); err != nil {
		return nil, err
	}
	defer a.unlock()
	user, err := login()
	if err != nil {
		return nil, err
	}
	a.tried = true
	if user == nil {
		return nil, fmt.Errorf("%s returned no user", a.Name)
	}
	return user, nil
}

// Logout logs out, also of the next runs: the saved login stays (a login picks it from the
// saved users) but isn't used on startup until the next login.
func (a *Account) Logout(ctx context.Context) error {
	if err := a.lock(ctx); err != nil {
		return err
	}
	defer a.unlock()
	a.tried = true
	if err := a.SetLoggedOut(true); err != nil {
		return fmt.Errorf("save the logout: %w", err)
	}
	return a.Drop()
}

// SetErr keeps why the login of this run failed (Err), nil when it didn't or it was
// rejected (the login is marked expired then) or cancelled.
func (a *Account) SetErr(ctx context.Context, err error, rejected bool) {
	a.errMu.Lock()
	defer a.errMu.Unlock()
	a.err = nil
	if err != nil && !rejected && ctx.Err() == nil {
		a.err = err
	}
}

// Err is why the login couldn't be checked this run (e.g. offline).
func (a *Account) Err() error {
	a.errMu.Lock()
	defer a.errMu.Unlock()
	return a.err
}

// Users are the users with a saved login, the last one logged in last.
func (a *Account) Users() ([]string, error) {
	var history cache.UserHistory
	if _, err := cache.GetAuthHistory(a.History, &history); err != nil {
		return nil, fmt.Errorf("get %s users: %w", a.Name, err)
	}
	return history.Get(), nil
}

// LastUser is the user of the startup login, empty after a logout or without users.
func (a *Account) LastUser() (string, error) {
	if out, err := a.LoggedOut(); err != nil || out {
		return "", err
	}
	users, err := a.Users()
	if err != nil || len(users) == 0 {
		return "", err
	}
	return users[len(users)-1], nil
}

// UpdateUsers changes the saved users.
func (a *Account) UpdateUsers(update func(*cache.UserHistory)) error {
	var history cache.UserHistory
	if _, err := cache.GetAuthHistory(a.History, &history); err != nil {
		return fmt.Errorf("get %s users: %w", a.Name, err)
	}
	update(&history)
	if err := cache.SetAuthHistory(a.History, history); err != nil {
		return fmt.Errorf("set %s users: %w", a.Name, err)
	}
	return nil
}

// NotFound drops the user without a saved login from the history, its error matches
// ErrUserNotFound.
func (a *Account) NotFound(user string) error {
	if err := a.UpdateUsers(func(h *cache.UserHistory) { h.Delete(user) }); err != nil {
		return err
	}
	return fmt.Errorf("%q: %w", user, ErrUserNotFound)
}

// LoggedIn makes the user the last one, so it logs in on startup again.
func (a *Account) LoggedIn(u metadata.User) error {
	if err := a.UpdateUsers(func(h *cache.UserHistory) { h.Add(u.Name()) }); err != nil {
		return err
	}
	if err := a.SetLoggedOut(false); err != nil {
		return fmt.Errorf("save the login: %w", err)
	}
	return nil
}

// Forget deletes the saved login of the user.
func (a *Account) Forget(user string) error {
	if err := a.DeleteToken(user); err != nil {
		return fmt.Errorf("delete login of %q: %w", user, err)
	}
	return a.UpdateUsers(func(h *cache.UserHistory) { h.Delete(user) })
}

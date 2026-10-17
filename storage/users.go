package storage

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// usersTables: the users who log into `mangal serve` (the web UI, the API, OPDS) and their
// sessions. A session is kept by the sha256 of its id; generation goes up when the user's
// password or permissions change, ending the sessions started before.
const usersTables = `CREATE TABLE serve_users (
	name       TEXT PRIMARY KEY COLLATE NOCASE,
	hash       TEXT NOT NULL,
	perms      TEXT NOT NULL,
	generation INTEGER NOT NULL DEFAULT 1,
	created    INTEGER NOT NULL,
	last_login INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE serve_sessions (
	id     TEXT PRIMARY KEY,
	data   BLOB NOT NULL,
	expiry INTEGER NOT NULL
)`

// ErrUserNotFound is a user that doesn't exist, ErrUserExists one that does.
var (
	ErrUserNotFound = errors.New("no such user")
	ErrUserExists   = errors.New("the user exists")
)

// User is a user of `mangal serve`.
type User struct {
	Name string
	// Hash is the password's argon2id hash (PHC string).
	Hash  string
	Perms []string
	// Generation ends the sessions of an older one (password or permissions changed).
	Generation int64
	Created    time.Time
	// LastLogin is zero when the user never logged in.
	LastLogin time.Time
}

// Users are the users of `mangal serve` and their sessions, in the data database.
type Users struct{ DB *DB }

// Users of `mangal serve`.
func (s Storage) Users() Users {
	return Users{DB: s.Data}
}

func scanUser(scan func(...any) error) (User, error) {
	var u User
	var perms string
	var created, last int64
	if err := scan(&u.Name, &u.Hash, &perms, &u.Generation, &created, &last); err != nil {
		return User{}, err
	}
	if perms != "" {
		u.Perms = strings.Split(perms, ",")
	}
	u.Created = time.Unix(created, 0)
	if last != 0 {
		u.LastLogin = time.Unix(last, 0)
	}
	return u, nil
}

const userColumns = `name, hash, perms, generation, created, last_login`

// List is every user, by name.
func (u Users) List() ([]User, error) {
	rows, err := u.DB.Query(`SELECT ` + userColumns + ` FROM serve_users ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list the users: %w", err)
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		user, err := scanUser(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("list the users: %w", err)
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

// Get is the user (names are case-insensitive), ErrUserNotFound if there's none.
func (u Users) Get(name string) (User, error) {
	user, err := scanUser(u.DB.QueryRow(`SELECT `+userColumns+` FROM serve_users WHERE name = ?`, name).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, fmt.Errorf("user %q: %w", name, ErrUserNotFound)
	}
	if err != nil {
		return User{}, fmt.Errorf("user %q: %w", name, err)
	}
	return user, nil
}

// Add adds the user, ErrUserExists if there's one with the name.
func (u Users) Add(user User) error {
	if user.Created.IsZero() {
		user.Created = time.Now()
	}
	if _, err := u.Get(user.Name); err == nil {
		return fmt.Errorf("add user %q: %w", user.Name, ErrUserExists)
	} else if !errors.Is(err, ErrUserNotFound) {
		return err
	}
	_, err := u.DB.Exec(`INSERT INTO serve_users (name, hash, perms, created) VALUES (?, ?, ?, ?)`,
		user.Name, user.Hash, strings.Join(user.Perms, ","), user.Created.Unix())
	if err != nil {
		return fmt.Errorf("add user %q: %w", user.Name, err)
	}
	return nil
}

// update changes a column of the user and ends its sessions (a new generation).
func (u Users) update(name, set string, args ...any) error {
	res, err := u.DB.Exec(`UPDATE serve_users SET `+set+`, generation = generation + 1 WHERE name = ?`, append(args, name)...)
	if err != nil {
		return fmt.Errorf("change user %q: %w", name, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("user %q: %w", name, ErrUserNotFound)
	}
	return nil
}

// SetHash changes the user's password hash, its sessions end.
func (u Users) SetHash(name, hash string) error {
	return u.update(name, `hash = ?`, hash)
}

// SetPerms changes the user's permissions, its sessions end.
func (u Users) SetPerms(name string, perms []string) error {
	return u.update(name, `perms = ?`, strings.Join(perms, ","))
}

// EndSessions ends the user's sessions.
func (u Users) EndSessions(name string) error {
	return u.update(name, `perms = perms`)
}

// LoggedIn records a login.
func (u Users) LoggedIn(name string, at time.Time) error {
	if _, err := u.DB.Exec(`UPDATE serve_users SET last_login = ? WHERE name = ?`, at.Unix(), name); err != nil {
		return fmt.Errorf("record the login of %q: %w", name, err)
	}
	return nil
}

// Remove removes the user (its sessions end with it).
func (u Users) Remove(name string) error {
	res, err := u.DB.Exec(`DELETE FROM serve_users WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("remove user %q: %w", name, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("user %q: %w", name, ErrUserNotFound)
	}
	return nil
}

// sessionKey is how a session id is kept: its sha256, so the table can't be replayed.
func sessionKey(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

// Sessions are the sessions of `mangal serve`'s logins (a scs.Store).
type Sessions struct{ DB *DB }

// Sessions of `mangal serve`'s users.
func (s Storage) Sessions() Sessions {
	return Sessions{DB: s.Data}
}

// Find is the session's data, found false when there's none or it expired.
func (s Sessions) Find(id string) ([]byte, bool, error) {
	var data []byte
	err := s.DB.QueryRow(`SELECT data FROM serve_sessions WHERE id = ? AND expiry > ?`, sessionKey(id), time.Now().UnixMilli()).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("find the session: %w", err)
	}
	return data, true, nil
}

// Commit saves the session until expiry.
func (s Sessions) Commit(id string, data []byte, expiry time.Time) error {
	_, err := s.DB.Exec(`INSERT INTO serve_sessions (id, data, expiry) VALUES (?, ?, ?)
ON CONFLICT (id) DO UPDATE SET data = excluded.data, expiry = excluded.expiry`, sessionKey(id), data, expiry.UnixMilli())
	if err != nil {
		return fmt.Errorf("save the session: %w", err)
	}
	return nil
}

// Delete removes the session.
func (s Sessions) Delete(id string) error {
	if _, err := s.DB.Exec(`DELETE FROM serve_sessions WHERE id = ?`, sessionKey(id)); err != nil {
		return fmt.Errorf("remove the session: %w", err)
	}
	return nil
}

// RemoveExpired removes the expired sessions.
func (s Sessions) RemoveExpired() error {
	if _, err := s.DB.Exec(`DELETE FROM serve_sessions WHERE expiry <= ?`, time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("remove the expired sessions: %w", err)
	}
	return nil
}

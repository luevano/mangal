package core

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
)

// The permissions of the users and the scopes of the API tokens: read is every GET and
// the events, watch changes the watches, progress records the reader's reads, write the
// files, the library, the config, the downloads.
const (
	ScopeRead     = "read"
	ScopeWatch    = "watch"
	ScopeProgress = "progress"
	ScopeWrite    = "write"
)

// Scopes are every scope.
var Scopes = []string{ScopeRead, ScopeWatch, ScopeProgress, ScopeWrite}

// NormalizeScopes are the scopes checked, sorted as Scopes, with read when there's any.
func NormalizeScopes(scopes []string) ([]string, error) {
	var out []string
	for _, s := range scopes {
		s = strings.TrimSpace(strings.ToLower(s))
		if s == "" {
			continue
		}
		if !slices.Contains(Scopes, s) {
			return nil, invalid("%q isn't a permission (%s)", s, strings.Join(Scopes, ", "))
		}
		out = append(out, s)
	}
	if len(out) != 0 {
		out = append(out, ScopeRead)
	}
	var sorted []string
	for _, s := range Scopes {
		if slices.Contains(out, s) {
			sorted = append(sorted, s)
		}
	}
	return sorted, nil
}

// MinPasswordLen is the shortest password a user can get.
const MinPasswordLen = 8

// HashPassword is how a user's password is kept: argon2id (PHC string).
func HashPassword(password string) (string, error) {
	if len(password) < MinPasswordLen {
		return "", invalid("the password needs %d characters or more", MinPasswordLen)
	}
	return argon2id.CreateHash(password, argon2id.DefaultParams)
}

// HashToken is how an API token is kept: the hex sha256.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NewToken is a random token (API tokens, the server's local key).
func NewToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// reservedTokenNames can't be API tokens' names (the identities that aren't tokens).
var reservedTokenNames = []string{"local", "none"}

// CheckUserName refuses a name a user can't have.
func CheckUserName(name string) error {
	if name == "" || strings.TrimSpace(name) != name || strings.ContainsAny(name, ":\x00") {
		return invalid("%q can't be a user's name (no spaces around it, no ':')", name)
	}
	return nil
}

// ErrLastAdmin is a change that would leave no user with every permission (nobody could
// manage the users from the web UI anymore).
var ErrLastAdmin = errors.New("it's the last user with every permission")

// User is a user who logs into mangal serve.
type User struct {
	Name  string   `json:"name"`
	Perms []string `json:"perms"`
	// Admin is true with every permission (the users can be managed).
	Admin   bool      `json:"admin"`
	Created time.Time `json:"created"`
	// LastLogin is zero when the user never logged in.
	LastLogin time.Time `json:"last_login,omitzero"`
}

// Token is an API token (its secret is only known when made).
type Token struct {
	Name    string    `json:"name"`
	Scopes  []string  `json:"scopes"`
	Created time.Time `json:"created"`
}

// Users manages the users who log into mangal serve and its API tokens, in the data db.
type Users interface {
	List(ctx context.Context) ([]User, error)
	// Add adds a user; no perms gives the first user every one, the others read and progress.
	Add(ctx context.Context, name, password string, perms []string) (User, error)
	SetPassword(ctx context.Context, name, password string) error
	SetPerms(ctx context.Context, name string, perms []string) error
	// EndLogins ends the user's sessions (every browser and app).
	EndLogins(ctx context.Context, name string) error
	Remove(ctx context.Context, name string) error

	Tokens(ctx context.Context) ([]Token, error)
	// AddToken makes an API token, secret is shown once; no scopes is read.
	AddToken(ctx context.Context, name string, scopes []string) (tok Token, secret string, err error)
	RemoveToken(ctx context.Context, name string) error
}

// NewUsers are the users and API tokens of this config's data db.
func NewUsers() Users { return localUsers{} }

// NewUsersOf are the users of the data db (the API tokens are this config's), for tests.
func NewUsersOf(users storage.Users) Users { return localUsers{users: &users} }

type localUsers struct {
	users *storage.Users
}

func (l localUsers) store() (storage.Users, error) {
	if l.users != nil {
		return *l.users, nil
	}
	st, err := cache.Storage()
	if err != nil {
		return storage.Users{}, err
	}
	return st.Users(), nil
}

func userOf(u storage.User) User {
	perms, _ := NormalizeScopes(u.Perms)
	if perms == nil {
		perms = []string{}
	}
	return User{Name: u.Name, Perms: perms, Admin: len(perms) == len(Scopes), Created: u.Created, LastLogin: u.LastLogin}
}

func notFound(err error) error {
	if errors.Is(err, storage.ErrUserNotFound) {
		return &NotFoundError{What: err.Error()}
	}
	return err
}

func (l localUsers) List(context.Context) ([]User, error) {
	st, err := l.store()
	if err != nil {
		return nil, err
	}
	list, err := st.List()
	if err != nil {
		return nil, err
	}
	users := make([]User, len(list))
	for i, u := range list {
		users[i] = userOf(u)
	}
	return users, nil
}

func (l localUsers) Add(ctx context.Context, name, password string, perms []string) (User, error) {
	if err := CheckUserName(name); err != nil {
		return User{}, err
	}
	st, err := l.store()
	if err != nil {
		return User{}, err
	}
	if _, err := st.Get(name); err == nil {
		return User{}, &RefusedError{Reasons: []string{fmt.Sprintf("there's a user %q already", name)}}
	} else if !errors.Is(err, storage.ErrUserNotFound) {
		return User{}, err
	}
	if len(perms) == 0 {
		list, err := st.List()
		if err != nil {
			return User{}, err
		}
		perms = []string{ScopeRead, ScopeProgress}
		if len(list) == 0 {
			perms = Scopes
		}
	}
	if perms, err = NormalizeScopes(perms); err != nil {
		return User{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	u := storage.User{Name: name, Hash: hash, Perms: perms, Created: time.Now()}
	if err := st.Add(u); err != nil {
		return User{}, err
	}
	return userOf(u), nil
}

func (l localUsers) SetPassword(_ context.Context, name, password string) error {
	st, err := l.store()
	if err != nil {
		return err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return notFound(st.SetHash(name, hash))
}

// keepsAdmin refuses a change of name's permissions (or its removal, perms nil) that
// leaves no user with every permission.
func keepsAdmin(name string, perms []string, removing bool) func([]storage.User) error {
	return func(list []storage.User) error {
		admins := 0
		isAdmin := false
		for _, u := range list {
			if userOf(u).Admin {
				admins++
				if strings.EqualFold(u.Name, name) {
					isAdmin = true
				}
			}
		}
		stays := !removing && len(perms) == len(Scopes)
		if isAdmin && admins == 1 && !stays {
			return &RefusedError{Reasons: []string{fmt.Sprintf("%s: %s", name, ErrLastAdmin)}}
		}
		return nil
	}
}

func (l localUsers) SetPerms(_ context.Context, name string, perms []string) error {
	perms, err := NormalizeScopes(perms)
	if err != nil {
		return err
	}
	if len(perms) == 0 {
		return invalid("no permissions given (read, progress, watch, write)")
	}
	st, err := l.store()
	if err != nil {
		return err
	}
	return notFound(st.SetPermsChecked(name, perms, keepsAdmin(name, perms, false)))
}

func (l localUsers) EndLogins(_ context.Context, name string) error {
	st, err := l.store()
	if err != nil {
		return err
	}
	return notFound(st.EndSessions(name))
}

func (l localUsers) Remove(_ context.Context, name string) error {
	st, err := l.store()
	if err != nil {
		return err
	}
	return notFound(st.RemoveChecked(name, keepsAdmin(name, nil, true)))
}

func (l localUsers) Tokens(context.Context) ([]Token, error) {
	entries, err := cache.ServeTokens()
	if err != nil {
		return nil, err
	}
	tokens := make([]Token, len(entries))
	for i, t := range entries {
		scopes, _ := NormalizeScopes(t.Scopes)
		tokens[i] = Token{Name: t.Name, Scopes: scopes, Created: t.Created}
	}
	return tokens, nil
}

func (l localUsers) AddToken(_ context.Context, name string, scopes []string) (Token, string, error) {
	if slices.Contains(reservedTokenNames, name) || strings.TrimSpace(name) != name || name == "" {
		return Token{}, "", invalid("%q can't be a token's name", name)
	}
	if len(scopes) == 0 {
		scopes = []string{ScopeRead}
	}
	scopes, err := NormalizeScopes(scopes)
	if err != nil {
		return Token{}, "", err
	}
	secret, err := NewToken()
	if err != nil {
		return Token{}, "", err
	}
	e := cache.ServeTokenEntry{Name: name, Hash: HashToken(secret), Scopes: scopes, Created: time.Now()}
	err = cache.UpdateServeTokens(func(entries []cache.ServeTokenEntry) ([]cache.ServeTokenEntry, error) {
		if slices.ContainsFunc(entries, func(t cache.ServeTokenEntry) bool { return t.Name == name }) {
			return nil, &RefusedError{Reasons: []string{fmt.Sprintf("there's a token %q already", name)}}
		}
		return append(entries, e), nil
	})
	if err != nil {
		return Token{}, "", err
	}
	return Token{Name: name, Scopes: scopes, Created: e.Created}, secret, nil
}

func (l localUsers) RemoveToken(_ context.Context, name string) error {
	return cache.UpdateServeTokens(func(entries []cache.ServeTokenEntry) ([]cache.ServeTokenEntry, error) {
		i := slices.IndexFunc(entries, func(t cache.ServeTokenEntry) bool { return t.Name == name })
		if i < 0 {
			return nil, &NotFoundError{What: fmt.Sprintf("no token %q", name)}
		}
		return slices.Delete(entries, i, i+1), nil
	})
}

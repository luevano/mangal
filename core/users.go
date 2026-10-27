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

// The permissions of the users and the API tokens (docs: Permissions). Some imply
// others (Permission.Implies), read is always there; admin is everything.
const (
	ScopeRead     = "read"
	ScopeProgress = "progress"
	ScopeBrowse   = "browse"
	ScopeDownload = "download"
	ScopeManage   = "manage"
	ScopeTokens   = "tokens"
	ScopeAdmin    = "admin"
)

// Permission is a permission and what it allows, as the CLI, the TUI and the web UI say it.
type Permission struct {
	Name string `json:"name"`
	Line string `json:"line"`
	// Implies are the permissions it brings along (read aside).
	Implies []string `json:"implies,omitempty"`
}

// Permissions are every permission, from the least to the most.
var Permissions = []Permission{
	{ScopeRead, "Browse the library and read the downloaded chapters (web reader, OPDS, files); see the watches, the jobs, the everyday settings and whether the server is fine. Nothing changes, no site is contacted.", nil},
	{ScopeProgress, "Keep your own reading position and read history (the server's AniList/Kitsu/MyAnimeList are only synced for admins).", nil},
	{ScopeBrowse, "Search the providers and metadata sites and read chapters online: the server contacts the sites for you.", nil},
	{ScopeDownload, "Download chapters from a provider with the server's settings (where, names, format); retry and cancel your own downloads.", []string{ScopeBrowse}},
	{ScopeManage, "Run the watches and fix the library one manga at a time: retitle, merge, move to another provider, links, fill, mark, trash; scan, health checks and fixes, run a job now, undo those; see and cancel anyone's jobs.", []string{ScopeDownload}},
	{ScopeTokens, "Make your own API tokens (for an OPDS app, a script), never with more than you have.", nil},
	{ScopeAdmin, "Everything, including what changes the server: users and every token, every setting, the log, the server's accounts, provider updates, pausing the jobs, library-wide operations (rename, migrate, empty the trash, clear the read history).", []string{ScopeProgress, ScopeManage, ScopeTokens}},
}

// Scopes are every permission's name.
var Scopes = func() []string {
	names := make([]string, len(Permissions))
	for i, p := range Permissions {
		names[i] = p.Name
	}
	return names
}()

// Role is a named set of permissions to pick from; a user's role isn't stored, it's the one
// their permissions match (else custom).
type Role struct {
	Name  string   `json:"name"`
	Line  string   `json:"line"`
	Perms []string `json:"perms"`
}

// Roles are the presets, Reader the default. tokens goes on top of any of them.
var Roles = []Role{
	{"reader", "Reads the library and keeps their place. Can't change anything or make the server contact a site.", []string{ScopeRead, ScopeProgress}},
	{"downloader", "Also searches the providers, reads online and downloads chapters with the server's settings.", []string{ScopeRead, ScopeProgress, ScopeBrowse, ScopeDownload}},
	{"manager", "Also runs the watches and fixes the library: retitle, merge, move to another provider, fill gaps, trash.", []string{ScopeRead, ScopeProgress, ScopeBrowse, ScopeDownload, ScopeManage}},
	{"admin", "Everything, including users, tokens, settings and operations on the whole library.", Scopes},
}

// RolePerms are the role's permissions.
func RolePerms(role string) ([]string, error) {
	for _, r := range Roles {
		if strings.EqualFold(r.Name, role) {
			return r.Perms, nil
		}
	}
	names := make([]string, len(Roles))
	for i, r := range Roles {
		names[i] = r.Name
	}
	return nil, invalid("%q isn't a role (%s)", role, strings.Join(names, ", "))
}

// WithRole are the role's permissions (none for no role) and perms on top.
func WithRole(role string, perms []string) ([]string, error) {
	if role == "" {
		return perms, nil
	}
	rp, err := RolePerms(role)
	if err != nil {
		return nil, err
	}
	return append(slices.Clone(rp), perms...), nil
}

// RoleOf names the permissions: a role, "<role> + tokens", or "custom".
func RoleOf(perms []string) string {
	for _, extra := range [][]string{nil, {ScopeTokens}} {
		for _, r := range Roles {
			want, _ := NormalizeScopes(append(slices.Clone(r.Perms), extra...))
			if slices.Equal(want, perms) {
				if extra != nil && r.Name != "admin" {
					return r.Name + " + tokens"
				}
				return r.Name
			}
		}
	}
	return "custom"
}

// oldScopes are the permission names before 5.28 still taken on input.
var oldScopes = map[string][]string{"watch": {ScopeManage}, "write": {ScopeDownload, ScopeManage}}

// NormalizeScopes are the permissions checked, with what they imply, sorted as Scopes,
// with read when there's any.
func NormalizeScopes(scopes []string) ([]string, error) {
	var out []string
	var add func(s string)
	add = func(s string) {
		if slices.Contains(out, s) {
			return
		}
		out = append(out, s)
		for _, p := range Permissions {
			if p.Name == s {
				for _, i := range p.Implies {
					add(i)
				}
			}
		}
	}
	for _, s := range scopes {
		s = strings.TrimSpace(strings.ToLower(s))
		if s == "" {
			continue
		}
		if old, ok := oldScopes[s]; ok {
			for _, o := range old {
				add(o)
			}
			continue
		}
		if !slices.Contains(Scopes, s) {
			return nil, invalid("%q isn't a permission (%s)", s, strings.Join(Scopes, ", "))
		}
		add(s)
	}
	if len(out) != 0 {
		add(ScopeRead)
	}
	var sorted []string
	for _, s := range Scopes {
		if slices.Contains(out, s) {
			sorted = append(sorted, s)
		}
	}
	return sorted, nil
}

// MigrateScopes are the permissions of before 5.28 in today's (storage.MigratePerms).
func MigrateScopes(old []string) []string { return storage.MigratePerms(old) }

// CapScopes are the permissions of scopes that have also has (a token can't have more than
// its owner).
func CapScopes(scopes, has []string) []string {
	return slices.DeleteFunc(slices.Clone(scopes), func(s string) bool { return !slices.Contains(has, s) })
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

// ErrLastAdmin is a change that would leave no admin (nobody could manage the users from
// the web UI anymore).
var ErrLastAdmin = errors.New("it's the last admin")

// User is a user who logs into mangal serve.
type User struct {
	Name  string   `json:"name"`
	Perms []string `json:"perms"`
	// Admin has the admin permission (the users can be managed).
	Admin bool `json:"admin"`
	// Role is the role the permissions match (RoleOf).
	Role    string    `json:"role"`
	Created time.Time `json:"created"`
	// LastLogin is zero when the user never logged in.
	LastLogin time.Time `json:"last_login,omitzero"`
}

// Token is an API token (its secret is only known when made).
type Token struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
	// Owner is the user who made it, empty for the server's (made from the CLI or by an
	// admin for nobody).
	Owner   string    `json:"owner,omitempty"`
	Created time.Time `json:"created"`
}

// TokenScopes are the token's permissions (the ones before 5.28 migrated).
func TokenScopes(t cache.ServeTokenEntry) []string {
	if t.Perms == nil {
		return MigrateScopes(t.Scopes)
	}
	scopes, _ := NormalizeScopes(t.Perms)
	return scopes
}

// Users manages the users who log into mangal serve and its API tokens, in the data db.
type Users interface {
	List(ctx context.Context) ([]User, error)
	// Add adds a user; no perms makes the first user an admin, the others readers.
	Add(ctx context.Context, name, password string, perms []string) (User, error)
	SetPassword(ctx context.Context, name, password string) error
	SetPerms(ctx context.Context, name string, perms []string) error
	// EndLogins ends the user's sessions (every browser and app).
	EndLogins(ctx context.Context, name string) error
	Remove(ctx context.Context, name string) error

	Tokens(ctx context.Context) ([]Token, error)
	// AddToken makes an API token owned by owner (empty for the server's), secret is
	// shown once; no scopes is read.
	AddToken(ctx context.Context, name, owner string, scopes []string) (tok Token, secret string, err error)
	SetTokenScopes(ctx context.Context, name string, scopes []string) error
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
	return User{Name: u.Name, Perms: perms, Admin: slices.Contains(perms, ScopeAdmin), Role: RoleOf(perms), Created: u.Created, LastLogin: u.LastLogin}
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
		perms = Roles[0].Perms
		if len(list) == 0 {
			perms = []string{ScopeAdmin}
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

// keepsAdmin refuses a change of name's permissions (or its removal) that leaves no admin.
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
		stays := !removing && slices.Contains(perms, ScopeAdmin)
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
		return invalid("no permissions given (%s)", strings.Join(Scopes, ", "))
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
	if err := notFound(st.RemoveChecked(name, keepsAdmin(name, nil, true))); err != nil {
		return err
	}
	// its tokens go with it (they'd have nothing left anyway)
	return cache.UpdateServeTokens(func(entries []cache.ServeTokenEntry) ([]cache.ServeTokenEntry, error) {
		return slices.DeleteFunc(entries, func(t cache.ServeTokenEntry) bool { return strings.EqualFold(t.Owner, name) }), nil
	})
}

func (l localUsers) Tokens(context.Context) ([]Token, error) {
	entries, err := cache.ServeTokens()
	if err != nil {
		return nil, err
	}
	tokens := make([]Token, len(entries))
	for i, t := range entries {
		tokens[i] = Token{Name: t.Name, Scopes: TokenScopes(t), Owner: t.Owner, Created: t.Created}
	}
	return tokens, nil
}

func (l localUsers) AddToken(_ context.Context, name, owner string, scopes []string) (Token, string, error) {
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
	e := cache.ServeTokenEntry{Name: name, Hash: HashToken(secret), Perms: scopes, Owner: owner, Created: time.Now()}
	err = cache.UpdateServeTokens(func(entries []cache.ServeTokenEntry) ([]cache.ServeTokenEntry, error) {
		if slices.ContainsFunc(entries, func(t cache.ServeTokenEntry) bool { return t.Name == name }) {
			return nil, &RefusedError{Reasons: []string{fmt.Sprintf("there's a token %q already", name)}}
		}
		return append(entries, e), nil
	})
	if err != nil {
		return Token{}, "", err
	}
	return Token{Name: name, Scopes: scopes, Owner: owner, Created: e.Created}, secret, nil
}

func (l localUsers) SetTokenScopes(_ context.Context, name string, scopes []string) error {
	scopes, err := NormalizeScopes(scopes)
	if err != nil {
		return err
	}
	if len(scopes) == 0 {
		return invalid("no permissions given (%s)", strings.Join(Scopes, ", "))
	}
	return cache.UpdateServeTokens(func(entries []cache.ServeTokenEntry) ([]cache.ServeTokenEntry, error) {
		i := slices.IndexFunc(entries, func(t cache.ServeTokenEntry) bool { return t.Name == name })
		if i < 0 {
			return nil, &NotFoundError{What: fmt.Sprintf("no token %q", name)}
		}
		entries[i].Perms = scopes
		return entries, nil
	})
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

type historyUserKey struct{}

// WithHistoryUser makes the reads and reading positions of the ctx's work the user's
// (serve's logins), empty for the server's own.
func WithHistoryUser(ctx context.Context, user string) context.Context {
	return context.WithValue(ctx, historyUserKey{}, user)
}

// HistoryUser is whose reads the ctx's work has, empty for the server's own.
func HistoryUser(ctx context.Context) string {
	user, _ := ctx.Value(historyUserKey{}).(string)
	return user
}

package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
)

// TestUsers: the first user is an admin, the next a reader; the last admin can't be
// demoted or removed; API tokens, a user's go with them.
func TestUsers(t *testing.T) {
	ctx := context.Background()
	u := NewUsers()
	list, err := u.List(ctx)
	if err != nil || len(list) != 0 {
		t.Fatalf("users %v %v", list, err)
	}
	if _, err := u.Add(ctx, "david", "short", nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("short password: %v", err)
	}
	if _, err := u.Add(ctx, " david", "correct horse", nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad name: %v", err)
	}
	admin, err := u.Add(ctx, "david", "correct horse", nil)
	if err != nil || !admin.Admin || len(admin.Perms) != len(Scopes) {
		t.Fatalf("first user %+v %v", admin, err)
	}
	guest, err := u.Add(ctx, "guest", "battery staple", nil)
	if err != nil || guest.Admin || !slices.Equal(guest.Perms, []string{ScopeRead, ScopeProgress}) {
		t.Fatalf("second user %+v %v", guest, err)
	}
	if _, err := u.Add(ctx, "David", "correct horse", nil); !errors.Is(err, ErrRefused) {
		t.Errorf("same name: %v", err)
	}
	if err := u.SetPerms(ctx, "david", []string{"read"}); !errors.Is(err, ErrRefused) {
		t.Errorf("demote the last admin: %v", err)
	}
	if err := u.Remove(ctx, "david"); !errors.Is(err, ErrRefused) {
		t.Errorf("remove the last admin: %v", err)
	}
	if err := u.SetPerms(ctx, "guest", Scopes); err != nil {
		t.Fatal(err)
	}
	if _, _, err := u.AddToken(ctx, "davids", "david", []string{ScopeRead}); err != nil {
		t.Fatal(err)
	}
	if err := u.Remove(ctx, "david"); err != nil {
		t.Errorf("remove with another admin: %v", err)
	}
	if tokens, _ := u.Tokens(ctx); len(tokens) != 0 {
		t.Errorf("a removed user's tokens stay: %+v", tokens)
	}
	if err := u.SetPassword(ctx, "nobody", "correct horse"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown user: %v", err)
	}
	if err := u.Remove(ctx, "guest"); !errors.Is(err, ErrRefused) {
		t.Errorf("remove the last one: %v", err)
	}

	tok, secret, err := u.AddToken(ctx, "claude", "", nil)
	if err != nil || secret == "" || !slices.Equal(tok.Scopes, []string{ScopeRead}) {
		t.Fatalf("token %+v %q %v", tok, secret, err)
	}
	if _, _, err := u.AddToken(ctx, "claude", "", nil); !errors.Is(err, ErrRefused) {
		t.Errorf("same token: %v", err)
	}
	if _, _, err := u.AddToken(ctx, "local", "", nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("reserved name: %v", err)
	}
	if tokens, err := u.Tokens(ctx); err != nil || len(tokens) != 1 {
		t.Errorf("tokens %v %v", tokens, err)
	}
	if err := u.RemoveToken(ctx, "claude"); err != nil {
		t.Fatal(err)
	}
	if err := u.RemoveToken(ctx, "claude"); !errors.Is(err, ErrNotFound) {
		t.Errorf("removed twice: %v", err)
	}
}

// TestTokensConcurrent: tokens added and removed at once all count (none lost, none back).
func TestTokensConcurrent(t *testing.T) {
	ctx := context.Background()
	u := NewUsers()
	if _, _, err := u.AddToken(ctx, "revoked", "", nil); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		if err := u.RemoveToken(ctx, "revoked"); err != nil {
			t.Error(err)
		}
	})
	for i := range 10 {
		wg.Go(func() {
			if _, _, err := u.AddToken(ctx, fmt.Sprint("t", i), "", nil); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	tokens, err := u.Tokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 10 || slices.ContainsFunc(tokens, func(t Token) bool { return t.Name == "revoked" }) {
		t.Errorf("tokens %v", tokens)
	}
	for _, tok := range tokens {
		if err := u.RemoveToken(ctx, tok.Name); err != nil {
			t.Error(err)
		}
	}
}

// TestLastAdminConcurrent: admins removed at once leave one.
func TestLastAdminConcurrent(t *testing.T) {
	ctx := context.Background()
	u := NewUsers()
	for _, name := range []string{"admin1", "admin2", "admin3"} {
		if _, err := u.Add(ctx, name, "correct horse", Scopes); err != nil {
			t.Fatal(err)
		}
	}
	list, err := u.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, user := range list {
		if user.Admin {
			wg.Go(func() {
				if err := u.Remove(ctx, user.Name); err != nil && !errors.Is(err, ErrRefused) {
					t.Error(err)
				}
			})
		}
	}
	wg.Wait()
	list, err = u.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	admins := 0
	for _, user := range list {
		if user.Admin {
			admins++
		}
	}
	if admins != 1 {
		t.Errorf("%d admins left: %+v", admins, list)
	}
}

// TestPermissions: what the permissions imply, the roles, the ones before 5.28.
func TestPermissions(t *testing.T) {
	for _, tc := range []struct {
		in, want []string
	}{
		{nil, nil},
		{[]string{"progress"}, []string{ScopeRead, ScopeProgress}},
		{[]string{"Download"}, []string{ScopeRead, ScopeBrowse, ScopeDownload}},
		{[]string{"manage"}, []string{ScopeRead, ScopeBrowse, ScopeDownload, ScopeManage}},
		{[]string{"admin"}, Scopes},
		{[]string{"write"}, []string{ScopeRead, ScopeBrowse, ScopeDownload, ScopeManage}},
	} {
		if got, err := NormalizeScopes(tc.in); err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("%v: %v %v", tc.in, got, err)
		}
	}
	if _, err := NormalizeScopes([]string{"everything"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("unknown: %v", err)
	}
	for _, tc := range []struct {
		old  []string
		want string
	}{
		{[]string{"read"}, "custom"},
		{[]string{"read", "progress"}, "custom"},
		{[]string{"read", "watch", "progress", "write"}, "admin"},
		{[]string{"read", "progress", "write"}, "manager"},
	} {
		if got := RoleOf(MigrateScopes(tc.old)); got != tc.want {
			t.Errorf("%v: %s (%v)", tc.old, got, MigrateScopes(tc.old))
		}
	}
	if got := MigrateScopes([]string{"read"}); !slices.Equal(got, []string{ScopeRead, ScopeBrowse}) {
		t.Errorf("read: %v", got)
	}
	perms, err := WithRole("downloader", []string{"tokens"})
	if err != nil {
		t.Fatal(err)
	}
	if perms, _ = NormalizeScopes(perms); RoleOf(perms) != "downloader + tokens" {
		t.Errorf("downloader + tokens: %s", RoleOf(perms))
	}
	if got := CapScopes(Scopes, []string{ScopeRead, ScopeBrowse}); !slices.Equal(got, []string{ScopeRead, ScopeBrowse}) {
		t.Errorf("cap: %v", got)
	}
}

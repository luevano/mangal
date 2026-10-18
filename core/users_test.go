package core

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// TestUsers: the first user gets every permission, the next read and progress; the last
// admin can't be demoted or removed; API tokens.
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
	if err := u.Remove(ctx, "david"); err != nil {
		t.Errorf("remove with another admin: %v", err)
	}
	if err := u.SetPassword(ctx, "nobody", "correct horse"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown user: %v", err)
	}
	if err := u.Remove(ctx, "guest"); !errors.Is(err, ErrRefused) {
		t.Errorf("remove the last one: %v", err)
	}

	tok, secret, err := u.AddToken(ctx, "claude", nil)
	if err != nil || secret == "" || !slices.Equal(tok.Scopes, []string{ScopeRead}) {
		t.Fatalf("token %+v %q %v", tok, secret, err)
	}
	if _, _, err := u.AddToken(ctx, "claude", nil); !errors.Is(err, ErrRefused) {
		t.Errorf("same token: %v", err)
	}
	if _, _, err := u.AddToken(ctx, "local", nil); !errors.Is(err, ErrInvalid) {
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

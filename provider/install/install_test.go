package install

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/provider/info"
	"github.com/luevano/mangal/v5/provider/manager"
)

// commit writes the file in the work repo, commits and pushes it to its origin.
func commit(t *testing.T, repo *git.Repository, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
		t.Fatal(err)
	}
	tree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tree.Add(name); err != nil {
		t.Fatal(err)
	}
	sig := &object.Signature{Name: "test", Email: "test@example.com", When: time.Now()}
	if _, err := tree.Commit(name, &git.CommitOptions{Author: sig}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Push(&git.PushOptions{RemoteName: "origin"}); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateEveryRepo(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	if _, err := git.PlainInit(origin, true); err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(root, "work")
	work, err := git.PlainInit(workDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := work.CreateRemote(&gitconfig.RemoteConfig{Name: "origin", URLs: []string{origin}}); err != nil {
		t.Fatal(err)
	}
	commit(t, work, workDir, "first")

	providers := filepath.Join(root, "providers")
	for _, name := range []string{"a", "b"} {
		if _, err := git.PlainClone(filepath.Join(providers, name), false, &git.CloneOptions{URL: origin}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(providers, "not-a-repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(providers, "file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	commit(t, work, workDir, "second")
	if err := Update(context.Background(), providers, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		if _, err := os.Stat(filepath.Join(providers, name, "second")); err != nil {
			t.Errorf("%s not updated: %s", name, err)
		}
	}

	// up to date
	if err := Update(context.Background(), providers, io.Discard); err != nil {
		t.Fatal(err)
	}
}

// originRepo is a bare repo with a provider pushed to it.
func originRepo(t *testing.T, root, id string) string {
	t.Helper()
	origin := filepath.Join(root, id+".git")
	if _, err := git.PlainInit(origin, true); err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(root, id+"-work")
	work, err := git.PlainInit(workDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := work.CreateRemote(&gitconfig.RemoteConfig{Name: "origin", URLs: []string{origin}}); err != nil {
		t.Fatal(err)
	}
	toml := fmt.Sprintf("type = \"lua\"\nid = %q\nname = \"X\"\nversion = \"0.1.0\"\n", id)
	if err := os.WriteFile(filepath.Join(workDir, info.Filename), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	tree, err := work.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tree.Add(info.Filename); err != nil {
		t.Fatal(err)
	}
	commit(t, work, workDir, "main.lua")
	return origin
}

func TestAdd(t *testing.T) {
	installed = func() ([]libmangal.ProviderLoader, error) { return nil, nil }
	t.Cleanup(func() { installed = manager.Loaders })

	root := t.TempDir()
	providers := filepath.Join(root, "providers")
	if err := os.Mkdir(providers, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := Add(context.Background(), providers, originRepo(t, root, "x"), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "x" {
		t.Errorf("id %q", got.ID)
	}
	if _, err := os.Stat(filepath.Join(providers, "x", "main.lua")); err != nil {
		t.Error(err)
	}

	// already there
	if _, err := Add(context.Background(), providers, originRepo(t, root, "x2"), io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(context.Background(), providers, filepath.Join(root, "x.git"), io.Discard); err == nil {
		t.Error("added x twice")
	}
	if _, err := Add(context.Background(), providers, filepath.Join(root, "missing.git"), io.Discard); err == nil {
		t.Error("no error for a missing repo")
	}

	entries, err := os.ReadDir(providers)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{"x", "x2"}) {
		t.Errorf("providers dir has %v", names)
	}
}

func TestAddBadID(t *testing.T) {
	installed = func() ([]libmangal.ProviderLoader, error) { return nil, nil }
	t.Cleanup(func() { installed = manager.Loaders })

	root := t.TempDir()
	providers := filepath.Join(root, "a", "providers")
	if err := os.MkdirAll(providers, 0o755); err != nil {
		t.Fatal(err)
	}
	origin := originRepo(t, root, "evil")
	// rewrite the id in a second commit
	work, err := git.PlainOpen(filepath.Join(root, "evil-work"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "evil-work", info.Filename), []byte("type = \"lua\"\nid = \"../x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commit(t, work, filepath.Join(root, "evil-work"), info.Filename)

	if _, err := Add(context.Background(), providers, origin, io.Discard); err == nil {
		t.Fatal("added a provider with id ../x")
	}
	if _, err := os.Stat(filepath.Join(root, "a", "x")); err == nil {
		t.Error("wrote outside the providers dir")
	}
}

func TestRemove(t *testing.T) {
	root := t.TempDir()
	providers := filepath.Join(root, "providers")
	if err := os.MkdirAll(filepath.Join(providers, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"", ".", "..", "../providers", "x/..", "/tmp", "missing"} {
		if err := Remove(providers, tag); err == nil {
			t.Errorf("removed %q", tag)
		}
	}
	if _, err := os.Stat(filepath.Join(providers, "x")); err != nil {
		t.Fatal(err)
	}
	if err := Remove(providers, "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(providers, "x")); err == nil {
		t.Error("x not removed")
	}
}

func TestNew(t *testing.T) {
	dir := t.TempDir()
	i := info.Info{
		ProviderInfo: libmangal.ProviderInfo{ID: "x", Name: "X", Version: "0.1.0", Website: "https://example.com"},
		Type:         info.TypeLua,
	}
	if err := New(dir, i); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{info.Filename, "main.lua", "sdk.lua", ".gitignore", "README.md", ".git"} {
		if _, err := os.Stat(filepath.Join(dir, "x", name)); err != nil {
			t.Error(err)
		}
	}
	got, err := readInfo(filepath.Join(dir, "x"))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "x" || got.Website != "https://example.com" {
		t.Errorf("info %+v", got)
	}
	if err := New(dir, i); err == nil {
		t.Error("overwrote x")
	}
	i.ID = "../y"
	if err := New(dir, i); err == nil {
		t.Error("created ../y")
	}
}

func TestCheckURL(t *testing.T) {
	for url, ok := range map[string]bool{
		"https://github.com/luevano/saturno":   true,
		"ssh://git@github.com/luevano/saturno": true,
		"git@github.com:luevano/saturno.git":   true,
		"file:///srv/git/x.git":                true,
		"/srv/git/x.git":                       true,
		`C:\git\x.git`:                         true,
		"http://github.com/luevano/saturno":    false,
		"git://github.com/luevano/saturno":     false,
		"HTTP://github.com/luevano/saturno":    false,
		"ftp://example.com/x.git":              false,
		"http://user:t0ken@example.com/x.git":  false,
	} {
		err := checkURL(url)
		if (err == nil) != ok {
			t.Errorf("%s: %v", url, err)
		}
		if err != nil && strings.Contains(err.Error(), "t0ken") {
			t.Errorf("%s: token in %v", url, err)
		}
	}
	if _, err := Add(context.Background(), t.TempDir(), "http://example.invalid/x.git", io.Discard); err == nil {
		t.Error("added over http")
	}
}

func TestRedact(t *testing.T) {
	const url = "https://user:t0ken@example.com/x.git"
	err := redact(fmt.Errorf("clone %s: %w", url, context.Canceled), url)
	if strings.Contains(err.Error(), "t0ken") || !errors.Is(err, context.Canceled) {
		t.Errorf("%v", err)
	}
	if err := redact(errors.New("auth for ghp_abc failed"), "https://ghp_abc@github.com/x"); strings.Contains(err.Error(), "ghp_abc") {
		t.Errorf("%v", err)
	}
}

// TestUpdateNonFastForward: a rewritten upstream history isn't forced over the provider.
func TestUpdateNonFastForward(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	if _, err := git.PlainInit(origin, true); err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(root, "work")
	work, err := git.PlainInit(workDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := work.CreateRemote(&gitconfig.RemoteConfig{Name: "origin", URLs: []string{origin}}); err != nil {
		t.Fatal(err)
	}
	commit(t, work, workDir, "first")
	providers := filepath.Join(root, "providers")
	if _, err := git.PlainClone(filepath.Join(providers, "a"), false, &git.CloneOptions{URL: origin}); err != nil {
		t.Fatal(err)
	}

	// another history, force-pushed
	otherDir := filepath.Join(root, "other")
	other, err := git.PlainInit(otherDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.CreateRemote(&gitconfig.RemoteConfig{Name: "origin", URLs: []string{origin}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, "rewritten"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	tree, _ := other.Worktree()
	if _, err := tree.Add("rewritten"); err != nil {
		t.Fatal(err)
	}
	sig := &object.Signature{Name: "test", Email: "test@example.com", When: time.Now()}
	if _, err := tree.Commit("rewritten", &git.CommitOptions{Author: sig}); err != nil {
		t.Fatal(err)
	}
	if err := other.Push(&git.PushOptions{RemoteName: "origin", Force: true}); err != nil {
		t.Fatal(err)
	}

	err = Update(context.Background(), providers, io.Discard)
	if !errors.Is(err, git.ErrNonFastForwardUpdate) || !strings.Contains(err.Error(), "add it again") {
		t.Errorf("update: %v", err)
	}
	if _, err := os.Stat(filepath.Join(providers, "a", "first")); err != nil {
		t.Errorf("provider replaced: %v", err)
	}
}

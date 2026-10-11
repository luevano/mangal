package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
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

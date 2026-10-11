package install

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
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

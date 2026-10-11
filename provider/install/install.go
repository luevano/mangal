// Package install installs, updates, removes and scaffolds the providers' git repositories.
package install

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/luevano/libmangal"
	"github.com/luevano/luaprovider"
	"github.com/luevano/mangal/v5/provider/info"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/samber/lo"
	"github.com/spf13/afero"
)

// installed are the installed providers, swapped in tests.
var installed = manager.Loaders

// Add clones the provider repository at url into dir (the providers dir), in a dir named
// after the provider id. The git progress goes to progress.
func Add(ctx context.Context, dir, url string, progress io.Writer) (_ info.Info, err error) {
	// cloned next to the providers, so the rename doesn't cross filesystems; dot dirs
	// aren't loaded as providers
	tempDir, err := os.MkdirTemp(dir, ".add-")
	if err != nil {
		return info.Info{}, err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(tempDir)
		}
	}()

	_, err = git.PlainCloneContext(ctx, tempDir, false, &git.CloneOptions{
		URL:      url,
		Progress: progress,
	})
	if err != nil {
		return info.Info{}, fmt.Errorf("clone %s: %w", url, err)
	}

	providerInfo, err := readInfo(tempDir)
	if err != nil {
		return info.Info{}, err
	}

	id := providerInfo.ID
	if err := checkName(id); err != nil {
		return info.Info{}, fmt.Errorf("provider id: %w", err)
	}

	loaders, err := installed()
	if err != nil {
		return info.Info{}, err
	}

	_, exists := lo.Find(loaders, func(loader libmangal.ProviderLoader) bool {
		return loader.Info().ID == id
	})
	if exists {
		return info.Info{}, fmt.Errorf("provider with id %q already exists", id)
	}

	target := filepath.Join(dir, id)
	if _, err := os.Lstat(target); err == nil {
		return info.Info{}, fmt.Errorf("%s already exists", target)
	}
	if err := os.Rename(tempDir, target); err != nil {
		return info.Info{}, err
	}
	return providerInfo, nil
}

// checkName errors if name isn't a plain dir name, so it can't point outside the providers dir.
func checkName(name string) error {
	switch {
	case name == "":
		return errors.New("empty")
	case name == "." || name == ".." || strings.HasPrefix(name, "."):
		return fmt.Errorf("%q starts with a dot", name)
	case strings.ContainsAny(name, `/\`) || filepath.Base(name) != name || filepath.VolumeName(name) != "":
		return fmt.Errorf("%q isn't a plain name", name)
	}
	return nil
}

func readInfo(dir string) (info.Info, error) {
	file, err := os.Open(filepath.Join(dir, info.Filename))
	if err != nil {
		return info.Info{}, err
	}
	defer file.Close()

	providerInfo, err := info.New(file)
	if err != nil {
		return info.Info{}, fmt.Errorf("read %s: %w", info.Filename, err)
	}
	return providerInfo, nil
}

// Update pulls every provider repository in dir, even if some fail. The git progress
// goes to progress.
func Update(ctx context.Context, dir string, progress io.Writer) error {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read providers dir: %w", err)
	}

	var errs []error
	for _, dirEntry := range dirEntries {
		if !dirEntry.IsDir() {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := pull(ctx, filepath.Join(dir, dirEntry.Name()), progress); err != nil {
			errs = append(errs, fmt.Errorf("update %s: %w", dirEntry.Name(), err))
		}
	}
	return errors.Join(errs...)
}

// pull updates the repository in dir, if it's one with a remote.
func pull(ctx context.Context, dir string, progress io.Writer) error {
	repo, err := git.PlainOpen(dir)
	if errors.Is(err, git.ErrRepositoryNotExists) {
		return nil
	}
	if err != nil {
		return err
	}

	worktree, err := repo.Worktree()
	if err != nil {
		return err
	}

	err = worktree.PullContext(ctx, &git.PullOptions{
		Progress: progress,
		Force:    true,
	})
	if errors.Is(err, git.NoErrAlreadyUpToDate) || errors.Is(err, git.ErrRemoteNotFound) {
		return nil
	}
	return err
}

// Remove removes the provider dir named tag from dir.
func Remove(dir, tag string) error {
	if err := checkName(tag); err != nil {
		return fmt.Errorf("remove provider: %w", err)
	}
	target := filepath.Join(dir, tag)
	if stat, err := os.Lstat(target); err != nil || !stat.IsDir() {
		return fmt.Errorf("provider %q not found in %s", tag, dir)
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("remove provider %q: %w", tag, err)
	}
	return nil
}

// New scaffolds a new provider repository in dir, in a dir named after its id.
func New(dir string, providerInfo info.Info) error {
	if err := checkName(providerInfo.ID); err != nil {
		return fmt.Errorf("provider id: %w", err)
	}
	target := filepath.Join(dir, providerInfo.ID)
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("%s already exists", target)
	}

	files := afero.Afero{Fs: afero.NewMemMapFs()}
	switch providerInfo.Type {
	case info.TypeLua:
		if err := newLua(files, providerInfo); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported provider type %v", providerInfo.Type)
	}

	readme, err := providerInfo.Markdown()
	if err != nil {
		return err
	}
	if err := files.WriteFile("README.md", []byte(readme), modeFile); err != nil {
		return err
	}

	return createRepo(target, files)
}

// modeFile is the mode of the scaffold files, like git checks them out.
const modeFile = 0o644

func newLua(af afero.Afero, information info.Info) error {
	err := af.WriteFile(".gitignore", []byte("sdk.lua"), modeFile)
	if err != nil {
		return err
	}

	infoFile, err := af.Create(info.Filename)
	if err != nil {
		return err
	}
	defer infoFile.Close()

	if err := information.Encode(infoFile); err != nil {
		return fmt.Errorf("write %s: %w", info.Filename, err)
	}

	err = af.WriteFile("main.lua", []byte(luaprovider.LuaTemplate()), modeFile)
	if err != nil {
		return err
	}

	return af.WriteFile("sdk.lua", []byte(luaprovider.LuaDoc()), modeFile)
}

func createRepo(dir string, files afero.Fs) error {
	err := afero.Walk(files, ".", func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if path == "" {
			return nil
		}

		dstPath := filepath.Join(dir, path)
		if info.IsDir() {
			return os.MkdirAll(dstPath, 0o755)
		}
		src, err := files.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()

		dst, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, modeFile)
		if err != nil {
			return err
		}
		defer dst.Close()

		_, err = io.Copy(dst, src)
		return err
	})
	if err != nil {
		return fmt.Errorf("write %s: %w", dir, err)
	}

	repo, err := git.PlainInit(dir, false)
	if err != nil {
		return fmt.Errorf("init repo: %w", err)
	}

	tree, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("init repo: %w", err)
	}

	if err := tree.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return fmt.Errorf("add files: %w", err)
	}
	return nil
}

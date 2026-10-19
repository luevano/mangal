// Package core is mangal's app services: what the CLI and the TUI (and later the serve
// API) do with the library, once. Every destructive change goes through Changes: a typed
// request, its Plan (lines, warnings, a hash), and Apply, which runs it as one undoable
// library operation (client.Ops: snapshot, journal) holding the library's lock, refusing
// a plan that went stale. The packages under it (client, library, storage, health) are
// the implementation; the UIs only adapt.
package core

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/health"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/template"
	"github.com/luevano/mangal/v5/util/afs"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/spf13/afero"
)

// MetadataFunc gets the AniList metadata of groups of ids (anilist.MetadataByIDs and its
// Batch, Cached and Fresh variants).
type MetadataFunc func(context.Context, [][]lmmeta.ID) ([]lmmeta.Metadata, error)

// Env is what the services work on. The zero fields are mangal's own: its databases, the
// OS file system, the configured download path, modes, providers and name templates,
// AniList's metadata. Built once per process (New), injectable for tests.
type Env struct {
	Storage storage.Storage
	FS      afero.Fs
	// Root is the download directory (download.path), read on each use.
	Root func() string
	// ProviderDirs is download.provider.create_dir.
	ProviderDirs func() bool
	// Modes are the file and folder modes of what's written.
	Modes func() library.Modes
	// BackupsDir is where the snapshots go, Backups how many are kept.
	BackupsDir string
	Backups    int
	// Loaders are the installed providers.
	Loaders func() ([]libmangal.ProviderLoader, error)
	// Watches are the watches (the running mangal serve's, else the local service),
	// connected when a change needs them.
	Watches func(ctx context.Context) (watch.Manager, error)
	// ProviderName, MangaName and ChapterName are the name templates.
	ProviderName func(libmangal.ProviderInfo) string
	MangaName    func(libmangal.ProviderInfo, mangadata.Manga) string
	ChapterName  func(libmangal.ProviderInfo, mangadata.Chapter) string
	// Metadata is the AniList metadata of a manga's ids (cached, looked up when not),
	// MetadataBatch of many mangas', MetadataCached only the cached, MetadataFresh looked
	// up again.
	Metadata                                     func(context.Context, []lmmeta.ID) (lmmeta.Metadata, error)
	MetadataBatch, MetadataCached, MetadataFresh MetadataFunc
	// CheckLink is the metadata of a source's id, to check it before linking a library
	// manga to it (nil metadata: not checked).
	CheckLink func(ctx context.Context, code lmmeta.IDCode, id string) (lmmeta.Metadata, error)
	// IDChecker checks the guessed metadata ids of folders (its FS is the Env's).
	IDChecker func(ctx context.Context) (library.IDChecker, error)
	// BindMetadata is the metadata of a source's id to bind a provider's manga to.
	BindMetadata func(ctx context.Context, code lmmeta.IDCode, id string) (lmmeta.Metadata, error)
	// BackupFinder finds the Kitsu and MyAnimeList links of mangas linked to AniList (its
	// Files are the Env's).
	BackupFinder func() (library.BackupFinder, error)
	// Checker is the health checker over the watches (health.NewWith).
	Checker func(watch.Manager) (health.Checker, error)
	// AllChapters finds a provider's manga (selector.Find's id, query and selector) and
	// lists every one of its chapters, unavailable, retired and duplicate ones too.
	AllChapters func(ctx context.Context, provider, id, query, sel string) (libmangal.ProviderInfo, mangadata.Manga, []mangadata.Chapter, error)
	// LogDir is the log files' directory.
	LogDir func() string
	// Now is the clock.
	Now func() time.Time
}

func (e Env) withDefaults() (Env, error) {
	if e.Storage.Data == nil {
		st, err := cache.Storage()
		if err != nil {
			return e, err
		}
		e.Storage = st
	}
	if e.FS == nil {
		e.FS = afs.Afero
	}
	if e.Root == nil {
		e.Root = path.Downloads
	}
	if e.ProviderDirs == nil {
		e.ProviderDirs = config.Download.Provider.CreateDir.Get
	}
	if e.Modes == nil {
		e.Modes = client.LibraryModes
	}
	if e.Loaders == nil {
		e.Loaders = manager.Loaders
	}
	if e.Watches == nil {
		e.Watches = connectWatches
	}
	if e.ProviderName == nil {
		e.ProviderName = template.Provider
	}
	if e.MangaName == nil {
		e.MangaName = template.Manga
	}
	if e.ChapterName == nil {
		e.ChapterName = template.Chapter
	}
	if e.Metadata == nil {
		e.Metadata = anilist.MetadataByIDs
	}
	if e.MetadataBatch == nil {
		e.MetadataBatch = anilist.MetadataByIDsBatch
	}
	if e.MetadataCached == nil {
		e.MetadataCached = anilist.MetadataByIDsCached
	}
	if e.MetadataFresh == nil {
		e.MetadataFresh = anilist.MetadataByIDsFresh
	}
	if e.CheckLink == nil {
		e.CheckLink = client.LinkMetadata
	}
	if e.IDChecker == nil {
		e.IDChecker = idChecker
	}
	if e.BindMetadata == nil {
		e.BindMetadata = client.BindMetadata
	}
	if e.BackupFinder == nil {
		e.BackupFinder = client.LibraryBackups
	}
	if e.Checker == nil {
		e.Checker = health.NewWith
	}
	if e.AllChapters == nil {
		e.AllChapters = allChapters
	}
	if e.LogDir == nil {
		e.LogDir = path.Logs
	}
	if e.Now == nil {
		e.Now = time.Now
	}
	return e, nil
}

// connectWatches is the running mangal serve's watches, else the local service.
func connectWatches(ctx context.Context) (watch.Manager, error) {
	m, err := watch.Connect(ctx, nil)
	if err != nil {
		return nil, err
	}
	if svc, ok := m.(*watch.Service); ok {
		svc.OnError = func(err error) { log.Warn("%s", err) }
	}
	return m, nil
}

// Core is the services over an Env. Each is an interface whose methods take a ctx and
// deal in plain data, so a remote implementation (the serve API's client) can stand in;
// the watches are watch.Manager's and the jobs whoever runs them (LocalJobs).
type Core struct {
	env Env
	ops client.Ops
	// Changes plans and applies the destructive changes.
	Changes Changes
	// Library reads the library.
	Library   Library
	Health    Health
	Logs      Logs
	Providers Providers
	Config    Config
	Accounts  Accounts
}

// New is the services over env (its zero fields mangal's own).
func New(_ context.Context, env Env) (*Core, error) {
	env, err := env.withDefaults()
	if err != nil {
		return nil, err
	}
	c := &Core{env: env, ops: client.Ops{Storage: env.Storage, BackupsDir: env.BackupsDir, Keep: env.Backups}}
	c.Changes = &changes{core: c}
	c.Library = &localLibrary{core: c}
	c.Health = &localHealth{core: c}
	c.Logs = NewLogs(env.LogDir)
	c.Providers = NewProviders(env.Loaders)
	c.Config = NewConfig()
	c.Accounts = NewAccounts()
	return c, nil
}

var defaultCore = sync.OnceValues(func() (*Core, error) { return New(context.Background(), Env{}) })

// Default is the services over mangal's own databases, files and config, made once per
// process.
func Default() (*Core, error) {
	return defaultCore()
}

// Watches are the watches: the running mangal serve's, else the local service.
func (c *Core) Watches(ctx context.Context) (watch.Manager, error) { return c.env.Watches(ctx) }

// Env is the environment the services work on (the defaults filled in).
func (c *Core) Env() Env { return c.env }

// files is the library over the download directory.
func (c *Core) files() library.Files {
	return library.Files{FS: c.env.FS, Library: c.env.Storage.Library(), Root: c.env.Root(), Modes: c.env.Modes()}
}

// existingFiles is files, an error when the download directory doesn't exist (a drive
// not mounted): nothing is planned on an empty one.
func (c *Core) existingFiles() (library.Files, error) {
	f := c.files()
	if err := existingDir(c.env.FS, f.Root); err != nil {
		return f, err
	}
	return f, nil
}

func (c *Core) undoer() library.Undoer {
	return library.Undoer{FS: c.env.FS, Journal: c.env.Storage.Journal(), Modes: c.env.Modes()}
}

// existingDir is nil when dir is an existing directory: the download path's errors of
// path.ExistingDownloads.
func existingDir(fsys afero.Fs, dir string) error {
	info, err := fsys.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%w: %s (drive not mounted?)", path.ErrNoDownloads, dir)
	case err != nil:
		return fmt.Errorf("download path: %w", err)
	case !info.IsDir():
		return fmt.Errorf("download path %s isn't a directory", dir)
	}
	return nil
}

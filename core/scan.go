package core

import (
	"context"

	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/client/kitsu"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/log"
)

// ScanOptions are what a library scan does besides reconciling the library with the
// files.
type ScanOptions struct {
	// Check looks the guessed ids of the folders without a .libmangal.json up (CheckIDs),
	// when the missing mangas aren't under another root; Statuses then refreshes the
	// publication statuses from AniList too.
	Check, Statuses bool
}

// ScanReport is what a library scan found.
type ScanReport struct {
	// Root is the download directory scanned.
	Root   string
	Result library.ScanResult
	// OtherRoot is an existing directory that has the missing mangas, "" if none.
	OtherRoot string
	// Duplicates are the library mangas sharing a metadata id.
	Duplicates []library.Duplicate
	// Checked is a scan with the id check: its Checks, or CheckErr.
	Checked  bool
	Checks   []library.IDCheck
	CheckErr error
}

// Scan reconciles the library with the download directory (an error when it doesn't
// exist: a drive not mounted), then finds the duplicates and, with opts.Check, checks the
// guessed ids. It changes the library's rows only (the files are never touched), as a
// download does: no operation.
func (l *Library) Scan(ctx context.Context, opts ScanOptions) (ScanReport, error) {
	c := l.core
	files, err := c.existingFiles()
	if err != nil {
		return ScanReport{Root: files.Root}, err
	}
	providerDirs := c.env.ProviderDirs()
	scanner := library.Scanner{FS: c.env.FS, Library: files.Library, OnProgress: progressOf(ctx)}
	rep := ScanReport{Root: files.Root}
	if rep.Result, err = scanner.ScanContext(ctx, files.Root, providerDirs); err != nil {
		return rep, err
	}
	rep.OtherRoot = library.OtherRoot(c.env.FS, rep.Result.Missing, files.Root, providerDirs)
	if rep.Duplicates, err = l.Duplicates(); err != nil {
		return rep, err
	}
	if opts.Check && rep.OtherRoot == "" {
		rep.Checked = true
		rep.Checks, rep.CheckErr = l.CheckIDs(ctx)
		if rep.CheckErr == nil && opts.Statuses {
			if mangas, err := files.Library.Mangas(); err == nil {
				rep.CheckErr = client.UpdateLibraryStatuses(ctx, files.Library, mangas, true, progressOf(ctx))
			}
		}
	}
	return rep, nil
}

// CheckIDs looks the metadata ids of the folders without a .libmangal.json up on AniList
// and Kitsu: those not found or whose manga doesn't look like the folder's, and an
// AniList suggestion for the folders without any.
func (l *Library) CheckIDs(ctx context.Context) ([]library.IDCheck, error) {
	c := l.core
	mangas, err := c.env.Storage.Library().Mangas()
	if err != nil {
		return nil, err
	}
	checker, err := c.env.IDChecker(ctx)
	if err != nil {
		return nil, err
	}
	checker.FS, checker.OnProgress = c.env.FS, progressOf(ctx)
	return checker.Check(ctx, mangas)
}

// idChecker checks AniList and Kitsu ids, and suggests AniList ones.
func idChecker(ctx context.Context) (library.IDChecker, error) {
	ani, err := anilist.Anilist()
	if err != nil {
		return library.IDChecker{}, err
	}
	if err := anilist.EnsureLogin(ctx); err != nil {
		// the checks go on without the login
		log.Debug("anilist login: %s", err)
	}
	kt, err := kitsu.Kitsu()
	if err != nil {
		return library.IDChecker{}, err
	}
	return library.IDChecker{
		Providers: map[lmmeta.IDSource]library.IDProvider{lmmeta.IDSourceAnilist: ani, lmmeta.IDSourceKitsu: kt},
		Suggest:   ani,
	}, nil
}

// Duplicates are the library mangas sharing a metadata id, with the one to keep suggested.
func (l *Library) Duplicates() ([]library.Duplicate, error) {
	return client.LibraryDuplicates(l.core.env.Storage.Library())
}

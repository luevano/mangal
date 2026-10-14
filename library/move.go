package library

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

// Mover plans moving library mangas to another provider.
type Mover struct {
	Files
	// ProviderDirs is download.provider.create_dir: the folder moves to the new provider's
	// directory.
	ProviderDirs bool
	// ProviderName and MangaName are the name templates.
	ProviderName func(libmangal.ProviderInfo) string
	MangaName    func(libmangal.ProviderInfo, mangadata.Manga) string
	// Metadata looks up the manga's metadata by one of its ids, nil if there's none.
	Metadata func(ctx context.Context, ids []metadata.ID) (metadata.Metadata, error)
}

// MoveOptions say how a library manga moves.
type MoveOptions struct {
	// Full adopts the new manga's title as the library title, and the folder name the name
	// template gives it. Otherwise both stay.
	Full bool
}

// MovePlan is what moving a library manga to another provider's manga does: the manga
// gets it as a source (its other sources stay), its folder goes to that provider's
// directory (with provider directories), its watch follows and the new manga gets its
// metadata bindings.
type MovePlan struct {
	Manga    storage.Manga
	Provider libmangal.ProviderInfo
	To       mangadata.MangaInfo
	// HasSource is a manga that has the source already.
	HasSource bool
	// Full is MoveOptions.Full, Title the library title after the move.
	Full  bool
	Title string
	// NameFromMetadata is a full move's folder named from the metadata (the template's
	// AniList title), not from the new manga's title.
	NameFromMetadata bool
	// From and Dir are the folder before and after, the same when it stays.
	From, Dir string
	// Root is the download directory, Lines shows the folders relative to it.
	Root string
	// ProviderDirName is the provider's directory name when the folder goes to the one of
	// its old name instead (only that one exists, downloads use it too).
	ProviderDirName string
	// Marker is the folder's new .libmangal.json, Old the one it had (nil if none);
	// nil for a manga without a folder of its own.
	Marker, Old *libmangal.MangaDirInfo
	// Watch is re-linked to the new manga, nil if none. Watched is the watch the new manga
	// has already (Watch is nil then).
	Watch, Watched *storage.Watch
	// Bindings are copied to the new manga (the sources it isn't bound to yet).
	Bindings []storage.Binding
}

// PlanMove plans moving the library manga to the provider's manga. A manga of another
// library manga is an error that says to merge them.
func (m Mover) PlanMove(ctx context.Context, manga storage.Manga, provider libmangal.ProviderInfo, to mangadata.Manga, opts MoveOptions) (MovePlan, error) {
	plan := MovePlan{Manga: manga, Provider: provider, To: to.Info(), From: manga.Path(), Dir: manga.Path(), Root: m.Root,
		Full: opts.Full, Title: manga.Title}
	if opts.Full && plan.To.Title != "" {
		plan.Title = plan.To.Title
	}
	fail := func(err error) (MovePlan, error) {
		return plan, fmt.Errorf("move %q to %s: %w", manga.Title, provider.ID, err)
	}
	ids := []string{provider.ID}
	for _, alias := range provider.History {
		ids = append(ids, alias.ID)
	}
	for _, id := range ids {
		other, found, err := m.Library.MangaBySource(id, plan.To.ID)
		if err != nil {
			return fail(err)
		}
		if found && other.ID != manga.ID {
			return fail(fmt.Errorf("%q is library manga %d %q already, merge them instead: mangal library merge %d %d",
				plan.To.Title, other.ID, other.Title, other.ID, manga.ID))
		}
		plan.HasSource = plan.HasSource || found && id == provider.ID
	}

	if manga.Dir != "" {
		if err := m.planFolder(ctx, &plan); err != nil {
			return fail(err)
		}
	}
	if err := m.planWatch(&plan); err != nil {
		return fail(err)
	}
	if err := m.planBindings(&plan); err != nil {
		return fail(err)
	}
	return plan, nil
}

// planFolder works out where the folder goes (its name changes only in a full move) and
// its new .libmangal.json.
func (m Mover) planFolder(ctx context.Context, plan *MovePlan) error {
	marker := libmangal.MangaDirInfo{MetadataIDs: plan.Manga.MetadataIDs}
	old, err := libmangal.ReadMangaDirInfo(m.FS, plan.From)
	switch {
	case err == nil:
		plan.Old = &old
		marker.MetadataIDs = old.MetadataIDs
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}
	marker.Provider, marker.Manga = plan.Provider.ID, plan.To
	plan.Marker = &marker

	parent := plan.Manga.Parent
	if m.ProviderDirs {
		if parent, err = m.providerDir(plan.Provider); err != nil {
			return err
		}
		if name := m.ProviderName(plan.Provider); filepath.Base(parent) != name {
			plan.ProviderDirName = name
		}
	}
	name := filepath.Base(plan.From)
	if plan.Full {
		ids := slices.Clone(marker.MetadataIDs)
		for _, id := range plan.Manga.MetadataIDs {
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		var meta metadata.Metadata
		if m.Metadata != nil {
			report(m.OnProgress, Progress{Phase: "looking up metadata"})
			if meta, err = m.Metadata(ctx, ids); err != nil {
				return fmt.Errorf("metadata: %w", err)
			}
		}
		plan.NameFromMetadata = metadata.Validate(meta) == nil
		name = m.MangaName(plan.Provider, &dirManga{info: plan.To, meta: meta})
	}
	plan.Dir = filepath.Join(parent, name)
	if plan.Dir == plan.From {
		return nil
	}
	if other, found, err := m.Library.MangaByFolder(filepath.Dir(plan.Dir), filepath.Base(plan.Dir)); err != nil {
		return err
	} else if found && other.ID != plan.Manga.ID {
		return fmt.Errorf("%s is the folder of library manga %d %q, merge them instead: mangal library merge %d %d",
			plan.Dir, other.ID, other.Title, other.ID, plan.Manga.ID)
	}
	if exists, err := afero.Exists(m.FS, plan.Dir); err != nil {
		return err
	} else if exists && !sameFile(m.FS, plan.From, plan.Dir) {
		return fmt.Errorf("%s already exists", plan.Dir)
	}
	return nil
}

// providerDir is the provider's directory, or the one of an old name when only that one
// exists (as downloads reuse it).
func (m Mover) providerDir(provider libmangal.ProviderInfo) (string, error) {
	dir := filepath.Join(m.Root, m.ProviderName(provider))
	if exists, err := afero.DirExists(m.FS, dir); err != nil || exists {
		return dir, err
	}
	for _, old := range OldInfos(provider) {
		oldDir := filepath.Join(m.Root, m.ProviderName(old))
		if exists, err := afero.DirExists(m.FS, oldDir); err != nil || exists {
			return oldDir, err
		}
	}
	return dir, nil
}

// planWatch picks the manga's watch to re-link: the one of the folder's source, else the
// first of its sources or linked to it.
func (m Mover) planWatch(plan *MovePlan) error {
	watches := storage.Watches{DB: m.Library.DB}
	if w, found, err := watches.Find(plan.Provider.ID, plan.To.ID); err != nil {
		return err
	} else if found {
		plan.Watched = &w
		return nil
	}
	list, err := watches.List()
	if err != nil {
		return err
	}
	for _, w := range list {
		ofManga := w.LibraryManga == plan.Manga.ID || slices.ContainsFunc(plan.Manga.Sources, func(src storage.Source) bool {
			return src.Provider == w.Provider && src.MangaID == w.MangaID
		})
		if !ofManga {
			continue
		}
		if plan.Watch == nil || plan.Old != nil && w.Provider == plan.Old.Provider && w.MangaID == plan.Old.Manga.ID {
			plan.Watch = &w
		}
	}
	return nil
}

// planBindings lists the bindings of the manga's sources (the folder's first) the new
// manga hasn't got.
func (m Mover) planBindings(plan *MovePlan) error {
	bindings := storage.Bindings{DB: m.Library.DB}
	have, err := bindings.Get(plan.Provider.ID, plan.To.ID)
	if err != nil {
		return err
	}
	sources := slices.Clone(plan.Manga.Sources)
	if plan.Old != nil {
		// the folder's source first
		slices.SortStableFunc(sources, func(a, b storage.Source) int {
			aOld := a.Provider == plan.Old.Provider && a.MangaID == plan.Old.Manga.ID
			bOld := b.Provider == plan.Old.Provider && b.MangaID == plan.Old.Manga.ID
			switch {
			case aOld && !bOld:
				return -1
			case bOld && !aOld:
				return 1
			}
			return 0
		})
	}
	bound := func(code metadata.IDCode) bool {
		return slices.ContainsFunc(have, func(b storage.Binding) bool { return b.Source == code }) ||
			slices.ContainsFunc(plan.Bindings, func(b storage.Binding) bool { return b.Source == code })
	}
	for _, src := range sources {
		if src.Provider == plan.Provider.ID && src.MangaID == plan.To.ID {
			continue
		}
		list, err := bindings.Get(src.Provider, src.MangaID)
		if err != nil {
			return err
		}
		for _, b := range list {
			if !bound(b.Source) {
				plan.Bindings = append(plan.Bindings, b)
			}
		}
	}
	storage.SortBindings(plan.Bindings)
	return nil
}

// Lines describe what the move does, one per line.
func (p MovePlan) Lines() []string {
	to := fmt.Sprintf("%s %q (%s)", p.Provider.ID, p.To.Title, p.To.ID)
	lines := []string{fmt.Sprintf("move %q (library manga %d) to %s", p.Manga.Title, p.Manga.ID, to)}
	var kept []string
	for _, src := range p.Manga.Sources {
		if src.Provider != p.Provider.ID || src.MangaID != p.To.ID {
			kept = append(kept, src.Provider+" "+src.MangaID)
		}
	}
	source := "  source: add " + p.Provider.ID + " " + p.To.ID
	if p.HasSource {
		source = "  source: has " + p.Provider.ID + " " + p.To.ID + " already"
	}
	if len(kept) != 0 {
		source += " (keeps " + strings.Join(kept, ", ") + ")"
	}
	lines = append(lines, source)
	if p.Full {
		lines = append(lines, "  title: "+p.TitleChange())
	}
	for _, line := range p.FolderLines() {
		lines = append(lines, "  "+line)
	}
	if p.Marker != nil {
		was := "none"
		if p.Old != nil {
			was = p.Old.Provider + " " + p.Old.Manga.ID
		}
		lines = append(lines, fmt.Sprintf("  %s: %s -> %s %s", libmangal.FilenameMangaDirInfo, was, p.Marker.Provider, p.Marker.Manga.ID))
	}
	switch {
	case p.Watch != nil:
		lines = append(lines, fmt.Sprintf("  watch %d: %s %s -> %s %s (re-linked)", p.Watch.ID, p.Watch.Provider, p.Watch.MangaID, p.Provider.ID, p.To.ID))
	case p.Watched != nil:
		lines = append(lines, fmt.Sprintf("  watch %d follows %s %s already", p.Watched.ID, p.Provider.ID, p.To.ID))
	}
	if len(p.Bindings) != 0 {
		var bindings []string
		for _, b := range p.Bindings {
			bindings = append(bindings, string(b.Source)+" "+b.ID)
		}
		lines = append(lines, "  metadata bindings copied: "+strings.Join(bindings, ", "))
	}
	return lines
}

// TitleChange is the library title's change: "old -> new", or unchanged.
func (p MovePlan) TitleChange() string {
	if p.Title == p.Manga.Title {
		return fmt.Sprintf("%q (unchanged)", p.Title)
	}
	return fmt.Sprintf("%q -> %q", p.Manga.Title, p.Title)
}

// FolderLines say what happens to the folder (paths relative to the root): "folder:"
// with "from" and "to" lines when it moves, else that it stays; in a full move why its
// name doesn't change, and when it goes to the provider's old directory.
func (p MovePlan) FolderLines() []string {
	rel := func(path string) string {
		if r, err := filepath.Rel(p.Root, path); err == nil && p.Root != "" && isInside(p.Root, path) {
			return r
		}
		return path
	}
	if p.Marker == nil {
		return []string{"folder: none of its own, the files stay in " + rel(p.From)}
	}
	var note string
	switch {
	case !p.Full || filepath.Base(p.Dir) != filepath.Base(p.From):
	case p.NameFromMetadata:
		note = "name unchanged: it uses the AniList title"
	default:
		note = "name unchanged"
	}
	if p.Dir == p.From {
		line := "folder: stays " + rel(p.From)
		if note != "" {
			line += " (" + note + ")"
		}
		return []string{line}
	}
	lines := []string{"folder:", "  from " + rel(p.From), "  to   " + rel(p.Dir)}
	if note != "" {
		lines = append(lines, "  "+note)
	}
	if p.ProviderDirName != "" {
		lines = append(lines, fmt.Sprintf("  %s is %s's old directory name, downloads use it too; \"mangal library rename\" renames it to %s",
			filepath.Base(filepath.Dir(p.Dir)), p.Provider.Name, p.ProviderDirName))
	}
	return lines
}

// ApplyMove does the plan: the folder moves and gets its new .libmangal.json, then the
// library, the watch and the bindings change in one transaction. When that fails the
// folder goes back (that rollback isn't journaled), see ApplyMerge for the many-files
// operations.
func (f Files) ApplyMove(plan MovePlan) error {
	return f.ApplyMoveContext(context.Background(), plan)
}

// ApplyMoveContext is ApplyMove, a copy across drives reporting its progress to ctx.
func (f Files) ApplyMoveContext(ctx context.Context, plan MovePlan) error {
	fail := func(err error) error {
		return fmt.Errorf("move %q to %s: %w", plan.Manga.Title, plan.Provider.ID, err)
	}
	moved := plan.Dir != plan.From
	if moved {
		if err := moveDir(ctx, f.FS, f.Journal, plan.From, plan.Dir, f.Modes); err != nil {
			return fail(err)
		}
	}
	undo := func(err error) error {
		switch {
		case plan.Old != nil:
			if undoErr := libmangal.WriteMangaDirInfo(f.FS, plan.Dir, *plan.Old, f.Modes.File); undoErr != nil {
				return fail(fmt.Errorf("%w (and restoring %s: %w)", err, libmangal.FilenameMangaDirInfo, undoErr))
			}
		case plan.Marker != nil:
			// it had none: the one written goes
			marker := filepath.Join(plan.Dir, libmangal.FilenameMangaDirInfo)
			if undoErr := f.FS.Remove(marker); undoErr != nil && !errors.Is(undoErr, fs.ErrNotExist) {
				return fail(fmt.Errorf("%w (and removing %s: %w)", err, libmangal.FilenameMangaDirInfo, undoErr))
			}
		}
		if moved {
			if undoErr := moveDir(ctx, f.FS, nil, plan.Dir, plan.From, f.Modes); undoErr != nil {
				return fail(fmt.Errorf("%w (and moving the folder back: %w)", err, undoErr))
			}
		}
		return fail(err)
	}
	if plan.Marker != nil {
		if err := writeDirInfo(f.FS, f.Journal, plan.Dir, *plan.Marker, f.Modes.File); err != nil {
			return undo(fmt.Errorf("write %s: %w", libmangal.FilenameMangaDirInfo, err))
		}
	}
	move := storage.MangaMove{
		Manga:    plan.Manga.ID,
		Source:   storage.Source{Provider: plan.Provider.ID, MangaID: plan.To.ID, Title: plan.To.Title},
		From:     plan.From,
		To:       plan.Dir,
		Title:    plan.Title,
		Bindings: plan.Bindings,
		At:       time.Now(),
	}
	if plan.Watch != nil {
		move.Watch = plan.Watch.ID
	}
	if err := f.Library.Move(move); err != nil {
		return undo(err)
	}
	return nil
}

// moveDir moves a folder (or a file), through a temporary name when only the case changes
// (on a case-insensitive filesystem), copied when it's on another drive (the copy goes
// when that fails). Something else at to is an error.
func moveDir(ctx context.Context, fsys afero.Fs, j Journal, from, to string, modes Modes) error {
	exists, err := afero.Exists(fsys, to)
	switch {
	case err != nil:
		return err
	case exists && sameFile(fsys, from, to):
		return renameCase(fsys, j, from, to)
	case exists:
		return fmt.Errorf("%s already exists", to)
	}
	return moveFile(ctx, fsys, j, from, to, modes)
}

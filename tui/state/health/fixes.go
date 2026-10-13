package health

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/health"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/confirm"
	"github.com/luevano/mangal/v5/util/afs"
	"github.com/luevano/mangal/v5/util/cache"
)

// libMsg is a confirmed change to the library, run with Load by the State (as runMsg).
type libMsg struct {
	label string
	do    func(ctx context.Context, status func(string)) (string, error)
}

func libChange(label string, do func(ctx context.Context, status func(string)) (string, error)) tea.Cmd {
	return func() tea.Msg { return libMsg{label: label, do: do} }
}

func (s *state) runLib(ctx context.Context, msg libMsg) tea.Cmd {
	return base.LoadWithStatus(ctx, msg.label, func(ctx context.Context, status func(string)) (changedMsg, error) {
		notify, err := msg.do(ctx, status)
		return changedMsg{notify: notify, lib: true}, err
	}, func(msg changedMsg) tea.Msg { return msg })
}

// renamePlannedMsg is the renames of a finding, planned again, to confirm.
type renamePlannedMsg struct {
	renames []library.Rename
	lines   []string
}

// undoable ends the notification of a journaled change.
var undoable = " (" + base.KeyUndo.Help().Key + " in the library undoes it)"

// applyLibrary asks to apply a library fix (mark, forget, scan, rename, empty the trash),
// false when the action isn't one.
func (s *state) applyLibrary(ctx context.Context, f health.Finding) (tea.Cmd, bool) {
	a := f.Fix.Action
	switch a.Kind {
	case health.ActionMark:
		ids := a.Mangas
		if a.Manga != 0 {
			ids = []int64{a.Manga}
		}
		message := fmt.Sprintf("Write the .libmangal.json of %q from the library?", f.Subject.Name)
		if len(ids) != 1 {
			message = fmt.Sprintf("Write the .libmangal.json of the %d folders from the library (what \"mangal library scan --mark --apply\" writes)?", len(ids))
		}
		s.confirm.Ask("Mark", message, s.View(), libChange("Marking", func(_ context.Context, status func(string)) (string, error) {
			return markMangas(ids, status)
		}))
	case health.ActionForget:
		id, name := a.Manga, f.Subject.Name
		s.confirm.Choose("Library manga", fmt.Sprintf("%s: %s\n\nScan the library (it finds folders that moved), or forget the manga?", f.Subject, f.Message), s.View(),
			confirm.Choice{Key: "u", Label: "scan the library", Cmd: libChange("Scanning the library", scanLibrary)},
			confirm.Choice{Key: "d", Label: "forget it", Cmd: libChange("Forgetting "+name, func(context.Context, func(string)) (string, error) {
				return forget(id)
			})})
	case health.ActionScan:
		s.confirm.Ask("Scan", "Scan the library? It adds the folders it doesn't have and finds the ones that moved.", s.View(),
			libChange("Scanning the library", scanLibrary))
	case health.ActionRename:
		paths := a.Paths
		return base.Load(ctx, "Planning the renames", func(ctx context.Context) (tea.Msg, error) {
			return planRenames(ctx, paths)
		}, func(msg tea.Msg) tea.Msg { return msg }), true
	case health.ActionEmptyTrash:
		s.confirm.AskDelete("Empty the trash", f.Message+"\n\nDelete them for good? It can't be undone.", s.View(),
			libChange("Emptying the trash", func(context.Context, func(string)) (string, error) {
				files, err := client.LibraryFiles()
				if err != nil {
					return "", err
				}
				if _, err := client.Snapshot("empty-trash"); err != nil {
					return "", err
				}
				return "trash emptied", library.EmptyTrash(files.FS, files.Root)
			}))
	default:
		return nil, false
	}
	return nil, true
}

// askRename asks to do the planned renames.
func (s *state) askRename(msg renamePlannedMsg) tea.Cmd {
	if len(msg.renames) == 0 {
		return base.Notify("Nothing to rename any more")
	}
	renames := msg.renames
	s.confirm.Ask("Rename", strings.Join(append(msg.lines, "", "Rename?"), "\n"), s.View(), libChange("Renaming", func(_ context.Context, status func(string)) (string, error) {
		return applyRenames(renames, status)
	}))
	return nil
}

// markMangas writes the .libmangal.json of the library mangas, as one operation.
func markMangas(ids []int64, status func(string)) (string, error) {
	files, err := client.LibraryFiles()
	if err != nil {
		return "", err
	}
	opts, err := client.LibraryMarkOptions()
	if err != nil {
		return "", err
	}
	var mangas []storage.Manga
	for _, id := range ids {
		m, ok, err := files.Library.MangaByID(id)
		if err != nil {
			return "", err
		}
		if !ok {
			continue
		}
		dry := opts
		dry.DryRun = true
		if _, do, err := files.MarkManga(m, dry); err != nil {
			return "", err
		} else if do {
			mangas = append(mangas, m)
		}
	}
	if len(mangas) == 0 {
		return "nothing to mark any more", nil
	}
	_, err = client.Operation("mark", fmt.Sprintf("mark %d folders", len(mangas)), func(j library.Journal) error {
		files.Journal = j
		for i, m := range mangas {
			status(fmt.Sprintf("marking: %d/%d (%s)", i+1, len(mangas), m.Title))
			if _, _, err := files.MarkManga(m, opts); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(mangas) == 1 {
		return "marked " + mangas[0].Title + undoable, nil
	}
	return fmt.Sprintf("marked %d folders%s", len(mangas), undoable), nil
}

// forget removes the library manga from the library (not its files), as an operation.
func forget(id int64) (string, error) {
	lib, err := cache.Library()
	if err != nil {
		return "", err
	}
	m, ok, err := lib.MangaByID(id)
	if err != nil || !ok {
		return "it's gone already", err
	}
	_, err = client.Operation("remove", fmt.Sprintf("remove %d %q", m.ID, m.Title), func(library.Journal) error {
		return lib.RemoveManga(m.ID)
	})
	if err != nil {
		return "", err
	}
	return "forgot " + m.Title + undoable, nil
}

// scanLibrary scans the library, the notification says what it found.
func scanLibrary(_ context.Context, status func(string)) (string, error) {
	files, err := client.LibraryFiles()
	if err != nil {
		return "", err
	}
	root, err := path.Ensure(files.Root)
	if err != nil {
		return "", err
	}
	providerDirs := config.Download.Provider.CreateDir.Get()
	scanner := library.Scanner{FS: files.FS, Library: files.Library, OnProgress: func(p library.Progress) { status(p.String()) }}
	res, err := scanner.Scan(root, providerDirs)
	if err != nil {
		return "", err
	}
	notify := fmt.Sprintf("scanned: %d chapters added, %d removed, %d folders new, %d moved", res.Added, res.Removed, res.New, res.Relocated)
	if n := len(res.Missing); n != 0 {
		notify += fmt.Sprintf(", %d still missing", n)
		if dir := library.OtherRoot(files.FS, res.Missing, root, providerDirs); dir != "" {
			notify += fmt.Sprintf(" (they're in %s: the library's scan offers to use it as download.path)", dir)
		}
	}
	return notify, nil
}

// planRenames plans library rename (cached metadata, as the finding) and keeps the
// renames of paths: manga folders, provider directories.
func planRenames(ctx context.Context, paths []string) (tea.Msg, error) {
	lib, err := cache.Library()
	if err != nil {
		return nil, err
	}
	namer, err := client.LibraryNamer(lib, anilist.MetadataByIDsCached)
	if err != nil {
		return nil, err
	}
	namer.NeedMetadata = true
	root := path.Downloads()
	all, err := namer.Plan(ctx, root, config.Download.Provider.CreateDir.Get())
	if err != nil {
		return nil, err
	}
	var msg renamePlannedMsg
	rel := func(p string) string {
		if r, err := filepath.Rel(root, p); err == nil {
			return r
		}
		return p
	}
	for _, r := range all {
		if !slices.Contains(paths, r.From) && !slices.Contains(paths, r.ProviderDir) {
			continue
		}
		msg.renames = append(msg.renames, r)
		switch {
		case r.Skip != "":
			msg.lines = append(msg.lines, fmt.Sprintf("skip %s: %s", rel(r.From), r.Skip))
		case r.RemoveEmpty:
			msg.lines = append(msg.lines, fmt.Sprintf("remove %s once empty", rel(r.From)))
		default:
			msg.lines = append(msg.lines, fmt.Sprintf("%s -> %s", rel(r.From), rel(r.To)))
		}
	}
	if !slices.ContainsFunc(msg.renames, func(r library.Rename) bool { return r.Skip == "" }) {
		msg.renames = nil
	}
	return msg, nil
}

// applyRenames does the renames as one operation.
func applyRenames(renames []library.Rename, status func(string)) (string, error) {
	lib, err := cache.Library()
	if err != nil {
		return "", err
	}
	done, skipped := 0, 0
	_, err = client.Operation("rename", fmt.Sprintf("rename %d folders", len(renames)), func(j library.Journal) error {
		if op, ok := j.(interface{ SetSummary(string) }); ok {
			defer func() { op.SetSummary(fmt.Sprintf("rename %d folders", done)) }()
		}
		return library.ApplyRenames(afs.Afero, lib, j, renames, func(p library.Progress) { status(p.String()) }, func(i int, err error) {
			switch {
			case errors.Is(err, library.ErrNotEmpty):
			case err != nil:
				skipped++
			case !renames[i].RemoveEmpty:
				done++
			}
		})
	})
	if err != nil {
		return "", err
	}
	notify := fmt.Sprintf("renamed %d folders", done)
	if skipped != 0 {
		notify += fmt.Sprintf(", %d skipped (mangal library rename says why)", skipped)
	}
	return notify + undoable, nil
}

// dismissedMsg is a finding dismissed (or brought back).
type dismissedMsg struct {
	key       string
	dismissed bool
}

// dismiss dismisses the finding (fine as it is), or brings a dismissed one back.
func (s *state) dismiss(ctx context.Context, f health.Finding) tea.Cmd {
	if f.Key == "" {
		return base.Notify("This finding can't be dismissed")
	}
	return base.Load(ctx, "", func(context.Context) (dismissedMsg, error) {
		check, subject, err := health.SplitKey(f.Key)
		if err != nil {
			return dismissedMsg{}, err
		}
		st, err := cache.Storage()
		if err != nil {
			return dismissedMsg{}, err
		}
		ignores := storage.HealthIgnores{DB: st.Data}
		if f.Dismissed {
			_, err = ignores.Undismiss(check, subject)
			return dismissedMsg{key: f.Key}, err
		}
		err = ignores.Dismiss(storage.HealthIgnore{Check: check, Subject: subject, Message: f.Message})
		return dismissedMsg{key: f.Key, dismissed: true}, err
	}, func(msg dismissedMsg) tea.Msg { return msg })
}

// onDismissed reloads (a summary updates its items), then notifies.
func (s *state) onDismissed(ctx context.Context, msg dismissedMsg) tea.Cmd {
	notify := "dismissed " + msg.key + " (" + s.keys.showAll.Help().Key + " shows the dismissed)"
	if !msg.dismissed {
		notify = "brought back " + msg.key
	}
	if s.summary == nil {
		return tea.Batch(s.load(ctx, false), base.Notify(notify))
	}
	for i, f := range s.report.Findings {
		if f.Key == msg.key {
			s.report.Findings[i].Dismissed = msg.dismissed
		}
	}
	if msg.dismissed {
		s.report.Findings = slices.DeleteFunc(s.report.Findings, func(f health.Finding) bool { return f.Key == msg.key })
	}
	return tea.Batch(s.setItems(), base.Notify(notify))
}

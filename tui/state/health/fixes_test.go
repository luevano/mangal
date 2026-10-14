package health

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/health"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/spf13/afero"
)

// the scan fix refuses a missing download.path (a drive not mounted), it doesn't make it
func TestScanLibraryMissingDownloads(t *testing.T) {
	root := filepath.Join(t.TempDir(), "dl")
	old := config.Download.Path.Get()
	if err := config.Download.Path.Set(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.Download.Path.Set(old) })
	if _, err := scanLibrary(context.Background(), func(string) {}); !errors.Is(err, path.ErrNoDownloads) {
		t.Errorf("missing download path: %v", err)
	}
	if _, err := os.Stat(root); err == nil {
		t.Error("made the missing download path")
	}
}

type fixWatches struct {
	watch.Manager
	enabled map[int64]bool
}

func (m *fixWatches) Get(_ context.Context, id int64) (watch.Watch, error) {
	return watch.Watch{Watch: storage.Watch{ID: id, Title: "Vagabond", Provider: "mango-mangadex", Enabled: m.enabled[id]}}, nil
}

func (m *fixWatches) SetEnabled(_ context.Context, id int64, enabled bool) (watch.Watch, error) {
	m.enabled[id] = enabled
	return m.Get(context.Background(), id)
}

// answer answers the screen's confirm with k, the msgs of what it runs.
func answer(t *testing.T, s *state, k string) []tea.Msg {
	t.Helper()
	if !s.Confirm.Active() {
		t.Fatal("not asking")
	}
	return base.Drain(s.Update(context.Background(), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}))
}

// update gives msgs to the screen, the msgs of what they run.
func update(s *state, msgs ...tea.Msg) []tea.Msg {
	var out []tea.Msg
	for _, msg := range msgs {
		out = append(out, base.Drain(s.Update(context.Background(), msg))...)
	}
	return out
}

func changed(t *testing.T, msgs []tea.Msg) changedMsg {
	t.Helper()
	for _, msg := range msgs {
		if c, ok := msg.(changedMsg); ok {
			return c
		}
		if err, ok := msg.(error); ok {
			t.Fatal(err)
		}
	}
	t.Fatalf("no change: %#v", msgs)
	return changedMsg{}
}

// the fixes go through health.Fixer (the serve API's): resume, forget, rename; the
// confirms stay the TUI's
func TestFixesThroughFixer(t *testing.T) {
	ctx := context.Background()
	data, err := storage.OpenData(filepath.Join(t.TempDir(), storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	lib := storage.Library{DB: data}
	if err := lib.RememberMangaDir("/dl", "Berserk", libmangal.MangaDirInfo{Provider: "p", Manga: mangadata.MangaInfo{ID: "b", Title: "Berserk"}}); err != nil {
		t.Fatal(err)
	}
	m, _, err := lib.MangaBySource("p", "b")
	if err != nil {
		t.Fatal(err)
	}
	fsys := afero.NewMemMapFs()
	from, to := "/dl/MangaDex/Berserk", "/dl/MangaDex/Berserk (1989)"
	_ = fsys.MkdirAll(from, 0o755)
	watches := &fixWatches{enabled: map[int64]bool{}}
	var ops []string
	old := newFixer
	t.Cleanup(func() { newFixer = old })
	newFixer = func(watch.Manager) (health.Fixer, error) {
		return health.Fixer{
			Watches: watches,
			Linker:  library.Linker{Files: library.Files{FS: fsys, Library: lib, Root: "/dl"}},
			Operation: func(kind, summary string, do func(library.Journal) error) (string, error) {
				ops = append(ops, kind+": "+summary)
				return "snap", do(nil)
			},
			Renames: func(context.Context) ([]library.Rename, error) {
				if ok, _ := afero.DirExists(fsys, from); !ok {
					return nil, nil
				}
				return []library.Rename{{From: from, To: to}}, nil
			},
		}, nil
	}
	s := newState(nil)
	s.Resize(base.Size{Width: 100, Height: 30})

	resume := health.Finding{Check: "W3", Subject: health.Subject{Kind: "watch", ID: 4, Name: "Vagabond"}, Fix: health.Fix{Action: &health.Action{Kind: health.ActionResume, Watch: 4}}}
	s.apply(ctx, resume)
	if c := changed(t, update(s, answer(t, s, "y")...)); c.notify != "resumed Vagabond" || c.lib || !watches.enabled[4] {
		t.Errorf("resume: %+v, enabled %v", c, watches.enabled)
	}

	forget := health.Finding{Check: "L1", Subject: health.Subject{Kind: "manga", ID: m.ID, Name: "Berserk"}, Fix: health.Fix{Action: &health.Action{Kind: health.ActionForget, Manga: m.ID}}}
	s.apply(ctx, forget)
	if c := changed(t, update(s, answer(t, s, "d")...)); !strings.HasPrefix(c.notify, "forgot Berserk (U in the library") || !c.lib {
		t.Errorf("forget: %+v", c)
	}
	if _, ok, _ := lib.MangaByID(m.ID); ok {
		t.Error("not forgotten")
	}

	rename := health.Finding{Check: "L4", Subject: health.Subject{Kind: "directory", Name: "MangaDex"}, Fix: health.Fix{Action: &health.Action{Kind: health.ActionRename, Paths: []string{from}}}}
	update(s, base.Drain(s.apply(ctx, rename))...)
	if v := s.Confirm.View(); !strings.Contains(v, "MangaDex/Berserk -> MangaDex/Berserk (1989)") {
		t.Fatalf("rename question:\n%s", v)
	}
	if c := changed(t, update(s, answer(t, s, "y")...)); !strings.HasPrefix(c.notify, "renamed 1 folder (U") {
		t.Errorf("rename: %+v", c)
	}
	if ok, _ := afero.DirExists(fsys, to); !ok {
		t.Error("not renamed")
	}
	// planned again: nothing left
	if msgs := update(s, base.Drain(s.apply(ctx, rename))...); s.Confirm.Active() || !slices.ContainsFunc(msgs, func(msg tea.Msg) bool {
		n, ok := msg.(base.NotificationMsg)
		return ok && n.Message == "Nothing to rename any more"
	}) {
		t.Errorf("renamed, asked again: %#v", msgs)
	}
	if want := []string{`remove: remove missing 1 "Berserk"`, "rename: rename 1 folders"}; !slices.Equal(ops, want) {
		t.Errorf("ops %q", ops)
	}
}

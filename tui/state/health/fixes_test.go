package health

import (
	"github.com/luevano/mangal/v5/tui/util"

	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
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
	return base.Drain(s.Update(context.Background(), util.KeyPress(k)))
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

// the fixes go through the core (as the CLI's and the serve API's): resume, forget,
// rename; the confirms stay the TUI's
func TestFixesThroughCore(t *testing.T) {
	ctx := context.Background()
	data, err := storage.OpenData(filepath.Join(t.TempDir(), storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	lib := storage.Library{DB: data}
	if err := lib.RememberMangaDir("/dl", "Gone", libmangal.MangaDirInfo{Provider: "p", Manga: mangadata.MangaInfo{ID: "b", Title: "Berserk"}}); err != nil {
		t.Fatal(err)
	}
	m, _, err := lib.MangaBySource("p", "b")
	if err != nil {
		t.Fatal(err)
	}
	fsys := afero.NewMemMapFs()
	from, to := "/dl/Berserk", "/dl/Berserk (1989) [alid-30002]"
	info := libmangal.MangaDirInfo{Provider: "p", Manga: mangadata.MangaInfo{ID: "b", Title: "Berserk"},
		MetadataIDs: []metadata.ID{{Raw: "30002", Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}}}
	if err := libmangal.WriteMangaDirInfo(fsys, from, info, 0o644); err != nil {
		t.Fatal(err)
	}
	watches := &fixWatches{enabled: map[int64]bool{}}
	meta := &mangadata.Metadata{EnglishTitle: "Berserk", AuthorList: []string{"Miura"}, DateStart: metadata.Date{Year: 1989, Month: 1, Day: 1},
		PublicationStatus: metadata.StatusReleasing, ProviderID: "30002", ProviderIDCode: string(metadata.IDCodeAnilist)}
	cached := func(_ context.Context, ids [][]metadata.ID) ([]metadata.Metadata, error) {
		metas := make([]metadata.Metadata, len(ids))
		for i := range ids {
			metas[i] = meta
		}
		return metas, nil
	}
	c, err := core.New(ctx, core.Env{
		Storage:        storage.Storage{Data: data},
		FS:             fsys,
		Root:           func() string { return "/dl" },
		ProviderDirs:   func() bool { return false },
		Modes:          func() library.Modes { return library.DefaultModes },
		BackupsDir:     t.TempDir(),
		Loaders:        func() ([]libmangal.ProviderLoader, error) { return nil, nil },
		Watches:        func(context.Context) (watch.Manager, error) { return watches, nil },
		MangaName:      func(_ libmangal.ProviderInfo, m mangadata.Manga) string { return m.Metadata().String() },
		MetadataCached: cached,
	})
	if err != nil {
		t.Fatal(err)
	}
	old := base.Core
	t.Cleanup(func() { base.Core = old })
	base.Core = func() (*core.Core, error) { return c, nil }
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

	rename := health.Finding{Check: "L5", Subject: health.Subject{Kind: "folder", Name: "Berserk"}, Fix: health.Fix{Action: &health.Action{Kind: health.ActionRename, Paths: []string{from}}}}
	update(s, base.Drain(s.apply(ctx, rename))...)
	if v := s.Confirm.View(); !strings.Contains(v, "Berserk -> Berserk (1989) [alid-30002]") {
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
	ops, _ := c.Library.Ops(ctx, 0)
	var kinds []string
	for _, op := range ops {
		kinds = append(kinds, op.Kind+": "+op.Summary)
	}
	if want := []string{"rename: rename 1 folders", `remove: remove missing 1 "Berserk"`}; !slices.Equal(kinds, want) {
		t.Errorf("ops %q", kinds)
	}
}

package library

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

func newMover(t *testing.T, root string) Mover {
	t.Helper()
	return Mover{
		Files:        Files{FS: afero.NewOsFs(), Library: newLibrary(t), Root: root, Modes: DefaultModes},
		ProviderDirs: true,
		ProviderName: func(p libmangal.ProviderInfo) string { return p.Name },
		MangaName: func(_ libmangal.ProviderInfo, m mangadata.Manga) string {
			if m.Metadata() != nil {
				return m.Metadata().Title()
			}
			return m.Info().Title
		},
	}
}

func TestMove(t *testing.T) {
	root := t.TempDir()
	m := newMover(t, root)
	lib := m.Library
	ctx := context.Background()
	asura := filepath.Join(root, "Asura")
	touch(t, filepath.Join(asura, "Berserk", "[0001.0] One.cbz"))
	writeMarker(t, filepath.Join(asura, "Berserk"), libmangal.MangaDirInfo{Provider: "asura", Manga: mangadata.MangaInfo{ID: "berserk", Title: "Berserk"},
		MetadataIDs: []metadata.ID{{Raw: "30002", Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}}})
	record(t, lib, asura, "Berserk", "[0001.0] One.cbz", 1, "asura")
	manga, _, _ := lib.MangaBySource("asura", "berserk")
	watches, bindings := storage.Watches{DB: lib.DB}, storage.Bindings{DB: lib.DB}
	w, _ := watches.Add(storage.Watch{Provider: "asura", MangaID: "berserk", Title: "Berserk", LibraryManga: manga.ID})
	_ = bindings.Set(storage.Binding{Provider: "asura", MangaID: "berserk", Source: "al", ID: "30002"})

	pill := libmangal.ProviderInfo{ID: "pill", Name: "Mangapill"}
	to := &dirManga{info: mangadata.MangaInfo{ID: "1/berserk", Title: "Berserk (Pill)"}}
	// a full move would adopt the new title and folder name
	full, err := m.PlanMove(ctx, manga, pill, to, MoveOptions{Full: true})
	if err != nil {
		t.Fatal(err)
	}
	if full.Title != "Berserk (Pill)" || full.Dir != filepath.Join(root, "Mangapill", "Berserk (Pill)") ||
		!strings.Contains(strings.Join(full.Lines(), "\n"), `title: "Berserk" -> "Berserk (Pill)"`) {
		t.Errorf("full plan %+v\n%s", full, strings.Join(full.Lines(), "\n"))
	}

	// the default keeps the title and the folder's name
	plan, err := m.PlanMove(ctx, manga, pill, to, MoveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	newDir := filepath.Join(root, "Mangapill", "Berserk")
	if plan.Dir != newDir || plan.Watch == nil || plan.Watch.ID != w.ID || len(plan.Bindings) != 1 || plan.Old == nil {
		t.Fatalf("plan %+v", plan)
	}
	lines := strings.Join(plan.Lines(), "\n")
	for _, want := range []string{"source: add pill 1/berserk (keeps asura berserk)", "folder: Asura/Berserk -> Mangapill/Berserk\n", ".libmangal.json: asura berserk -> pill 1/berserk", "(re-linked)", "copied: al 30002"} {
		if !strings.Contains(lines, want) {
			t.Errorf("lines miss %q:\n%s", want, lines)
		}
	}
	if err := m.ApplyMove(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(newDir, "[0001.0] One.cbz")); err != nil {
		t.Error(err)
	}
	info, err := libmangal.ReadMangaDirInfo(m.FS, newDir)
	if err != nil || info.Provider != "pill" || info.Manga.ID != "1/berserk" || len(info.MetadataIDs) != 1 {
		t.Errorf("marker %+v, %v", info, err)
	}
	got, _, _ := lib.MangaByID(manga.ID)
	if got.Path() != newDir || len(got.Sources) != 2 || got.Title != "Berserk" {
		t.Errorf("manga %+v", got)
	}
	if ok, _ := lib.HasDownload(filepath.Join(newDir, "[0001.0] One.cbz")); !ok {
		t.Error("download path not updated")
	}
	if gotW, _ := watches.Get(w.ID); gotW.Provider != "pill" || gotW.MangaID != "1/berserk" {
		t.Errorf("watch %+v", gotW)
	}
	if list, _ := bindings.Get("pill", "1/berserk"); len(list) != 1 || list[0].ID != "30002" {
		t.Errorf("bindings %+v", list)
	}

	// another library manga has the new provider's manga: merge them instead
	touch(t, filepath.Join(asura, "Vagabond", "1.cbz"))
	if err := lib.RecordDownload(asura, "Vagabond", storage.Download{Path: filepath.Join(asura, "Vagabond", "1.cbz"), Provider: "asura", MangaID: "vagabond", MangaTitle: "Vagabond", Chapter: 1, Format: "CBZ"}); err != nil {
		t.Fatal(err)
	}
	vagabond, _, _ := lib.MangaBySource("asura", "vagabond")
	if _, err := m.PlanMove(ctx, vagabond, pill, to, MoveOptions{}); err == nil || !strings.Contains(err.Error(), "mangal library merge") {
		t.Errorf("onto another manga: %v", err)
	}
}

// TestMoveUndo: when the library can't change, the folder and its .libmangal.json go back.
func TestMoveUndo(t *testing.T) {
	root := t.TempDir()
	m := newMover(t, root)
	dir := filepath.Join(root, "Asura", "Berserk")
	touch(t, filepath.Join(dir, "1.cbz"))
	old := libmangal.MangaDirInfo{Provider: "asura", Manga: mangadata.MangaInfo{ID: "berserk", Title: "Berserk"}}
	writeMarker(t, dir, old)
	record(t, m.Library, filepath.Dir(dir), "Berserk", "1.cbz", 1, "asura")
	manga, _, _ := m.Library.MangaBySource("asura", "berserk")
	plan, err := m.PlanMove(context.Background(), manga, libmangal.ProviderInfo{ID: "pill", Name: "Mangapill"}, &dirManga{info: mangadata.MangaInfo{ID: "1", Title: "Berserk"}}, MoveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	plan.Manga.ID = 999 // no such manga: the source can't be added
	if err := m.ApplyMove(plan); err == nil {
		t.Fatal("no error")
	}
	if info, err := libmangal.ReadMangaDirInfo(m.FS, dir); err != nil || info.Provider != "asura" {
		t.Errorf("marker %+v, %v", info, err)
	}
	if _, err := os.Stat(plan.Dir); !os.IsNotExist(err) {
		t.Errorf("new folder left: %v", err)
	}
}

func TestMoveFolderChange(t *testing.T) {
	marker := &libmangal.MangaDirInfo{}
	plan := MovePlan{Root: "/dl", From: "/dl/Asura/Berserk", Dir: "/dl/Pill/Berserk", Marker: marker, Full: true, NameFromMetadata: true}
	if got := plan.FolderChange(); got != "Asura/Berserk -> Pill/Berserk (name unchanged: it uses the AniList title)" {
		t.Errorf("metadata name: %q", got)
	}
	plan.NameFromMetadata = false
	plan.Dir = plan.From
	if got := plan.FolderChange(); got != "stays Asura/Berserk (name unchanged)" {
		t.Errorf("same name: %q", got)
	}
	plan.Full = false
	if got := plan.FolderChange(); got != "stays Asura/Berserk" {
		t.Errorf("default: %q", got)
	}
}

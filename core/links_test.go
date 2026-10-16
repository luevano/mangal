package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
)

type testManga struct{ info mangadata.MangaInfo }

func (m *testManga) String() string                { return m.info.Title }
func (m *testManga) Info() mangadata.MangaInfo     { return m.info }
func (m *testManga) Metadata() metadata.Metadata   { return nil }
func (m *testManga) SetMetadata(metadata.Metadata) {}

func testMeta(title, al, mal string) metadata.Metadata {
	m := &mangadata.Metadata{EnglishTitle: title, AuthorList: []string{"Miura"}, DateStart: metadata.Date{Year: 1989, Month: 1, Day: 1},
		PublicationStatus: metadata.StatusReleasing, ProviderID: al, ProviderIDCode: string(metadata.IDCodeAnilist)}
	if mal != "" {
		m.OtherIDs = []metadata.ID{{Raw: mal, Source: metadata.IDSourceMyAnimeList, Code: metadata.IDCodeMyAnimeList}}
	}
	return m
}

// testLinkCore is testCore with provider directories, names from the manga's title and
// AniList 30002 as the only metadata.
func testLinkCore(t *testing.T) (*Core, string) {
	t.Helper()
	c, root := testCore(t)
	env := c.env
	env.ProviderDirs = func() bool { return true }
	env.ProviderName = func(p libmangal.ProviderInfo) string { return p.Name }
	env.MangaName = func(_ libmangal.ProviderInfo, m mangadata.Manga) string {
		if meta := m.Metadata(); metadata.Validate(meta) == nil {
			return meta.String()
		}
		return m.Info().Title
	}
	env.Metadata = func(_ context.Context, ids []metadata.ID) (metadata.Metadata, error) {
		for _, id := range ids {
			if id.Raw == "30002" {
				return testMeta("Berserk", "30002", "2"), nil
			}
		}
		return nil, nil
	}
	env.MetadataBatch = func(ctx context.Context, ids [][]metadata.ID) ([]metadata.Metadata, error) {
		var metas []metadata.Metadata
		for _, group := range ids {
			m, _ := env.Metadata(ctx, group)
			metas = append(metas, m)
		}
		return metas, nil
	}
	env.MetadataCached, env.MetadataFresh = env.MetadataBatch, env.MetadataBatch
	env.CheckLink = func(_ context.Context, _ metadata.IDCode, id string) (metadata.Metadata, error) {
		if id == "404" {
			return nil, errors.New("AniList 404: not found")
		}
		return nil, nil
	}
	env.BackupFinder = func() (library.BackupFinder, error) {
		return library.BackupFinder{AniList: env.MetadataBatch}, nil
	}
	c, err := New(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	return c, root
}

func TestMove(t *testing.T) {
	c, root := testLinkCore(t)
	ctx := context.Background()
	m := addManga(t, c, filepath.Join("Asura Scans", "Berserk"), "mango-asurascans", "b", "Berserk", 1)
	req := MoveReq{Manga: m.ID, Provider: libmangal.ProviderInfo{ID: "mango-weebcentral", Name: "Weeb Central"}, To: &testManga{mangadata.MangaInfo{ID: "01J", Title: "Berserk"}}}
	p, err := c.Changes.Plan(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(p.Lines, "\n"), filepath.Join("Weeb Central", "Berserk")) {
		t.Fatalf("lines %q", p.Lines)
	}
	if _, err := c.Changes.ApplyPlan(ctx, p, ApplyOpts{}); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(root, "Weeb Central", "Berserk", "[0001.0] Chapter 1.cbz")) {
		t.Fatal("not moved")
	}
	if got, _ := c.Library.Manga(t.Context(), m.ID); len(got.Sources) != 2 {
		t.Errorf("sources %+v", got.Sources)
	}
	// moved already: the same plan again is stale
	if _, err := c.Changes.ApplyPlan(ctx, p, ApplyOpts{}); err == nil {
		t.Error("applied twice")
	}
	undoLast(t, c)
	if !exists(filepath.Join(root, "Asura Scans", "Berserk", "[0001.0] Chapter 1.cbz")) {
		t.Fatal("not undone")
	}
	if _, err := c.Changes.Plan(ctx, MoveReq{Manga: m.ID}); !errors.Is(err, ErrInvalid) {
		t.Errorf("nowhere to move: %v", err)
	}
}

func TestLink(t *testing.T) {
	c, root := testLinkCore(t)
	ctx := context.Background()
	m := addManga(t, c, filepath.Join("Asura Scans", "Berserk"), "mango-asurascans", "b", "Berserk", 1)
	if _, err := c.Changes.Plan(ctx, LinkReq{Manga: m.ID, Links: library.Links{metadata.IDCodeAnilist: "404"}}); err == nil {
		t.Fatal("a wrong id isn't checked")
	}
	p, err := c.Changes.Plan(ctx, LinkReq{Manga: m.ID, Links: library.Links{metadata.IDCodeAnilist: "30002"}, Rename: true})
	if err != nil {
		t.Fatal(err)
	}
	d := p.Detail.(LinkDetail)
	if len(d.Checked) != 1 || !d.Renames || p.Lines[len(p.Lines)-1] != "rename to Berserk (1989) [alid-30002]" {
		t.Fatalf("plan %q %+v", p.Lines, d)
	}
	if _, err := c.Changes.ApplyPlan(ctx, p, ApplyOpts{}); err != nil {
		t.Fatal(err)
	}
	renamed := filepath.Join(root, "Asura Scans", "Berserk (1989) [alid-30002]")
	if !exists(renamed) {
		t.Fatal("not renamed")
	}
	// linked: nothing to do the same again
	if again, err := c.Changes.Plan(ctx, LinkReq{Manga: m.ID, Links: library.Links{metadata.IDCodeAnilist: "30002"}}); err != nil || !again.Nothing {
		t.Errorf("again: %+v %v", again, err)
	}

	// the backups: MyAnimeList from AniList
	p, err = c.Changes.Plan(ctx, BackupsReq{})
	if err != nil {
		t.Fatal(err)
	}
	if found := p.Detail.(BackupsDetail); found.ToSet != 1 || found.Fill != 1 || p.Summary != "backup links of 1 mangas" {
		t.Fatalf("backups %+v\n%q", found, p.Lines)
	}
	res, err := c.Changes.ApplyPlan(ctx, p, ApplyOpts{})
	if err != nil || res.Detail.(int) != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	if p, _ := c.Changes.Plan(ctx, BackupsReq{}); !p.Nothing {
		t.Errorf("backups again: %q", p.Lines)
	}

	undoLast(t, c)
	undoLast(t, c)
	if !exists(filepath.Join(root, "Asura Scans", "Berserk")) {
		t.Fatal("rename not undone")
	}
	if _, err := os.Stat(renamed); err == nil {
		t.Error("renamed folder left")
	}
}

func TestVariants(t *testing.T) {
	c, root := testLinkCore(t)
	ctx := context.Background()
	m := addManga(t, c, filepath.Join("Asura Scans", "Berserk"), "mango-asurascans", "b", "Berserk", 1)
	req := LinkReq{Manga: m.ID, Links: library.Links{metadata.IDCodeAnilist: "30002"}}
	checks := 0
	env := c.env
	env.CheckLink = func(context.Context, metadata.IDCode, string) (metadata.Metadata, error) {
		checks++
		return nil, nil
	}
	c, _ = New(ctx, env)
	p, err := c.Changes.Plan(ctx, req)
	if err != nil || p.Detail.(LinkDetail).Renames {
		t.Fatalf("%+v %v", p, err)
	}
	req.Rename = true
	v, err := c.Changes.Variant(ctx, p, req)
	if err != nil {
		t.Fatal(err)
	}
	if checks != 1 || v.Hash == p.Hash || !v.Detail.(LinkDetail).Renames || v.Lines[len(v.Lines)-1] != "rename to Berserk (1989) [alid-30002]" {
		t.Fatalf("variant %q (%d checks)", v.Lines, checks)
	}
	// the variant hashes as the change planned anew
	if again, _ := c.Changes.Plan(ctx, req); again.Hash != v.Hash {
		t.Errorf("hash %s, planned anew %s", v.Hash, again.Hash)
	}
	// another manga's links aren't a variant: planned anew
	if _, err := c.Changes.Variant(ctx, p, LinkReq{Manga: m.ID, Links: library.Links{metadata.IDCodeAnilist: "30003"}}); err != nil || checks != 3 {
		t.Errorf("not a variant: %v (%d checks)", err, checks)
	}
	if _, err := c.Changes.ApplyPlan(ctx, v, ApplyOpts{}); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(root, "Asura Scans", "Berserk (1989) [alid-30002]")) {
		t.Fatal("not renamed")
	}
	undoLast(t, c)

	// a variant of a plan the library moved on from is refused
	p, _ = c.Changes.Plan(ctx, BackupsReq{Mangas: []int64{m.ID}})
	addManga(t, c, "Other", "p", "o", "Other", 1)
	if _, err := c.Changes.Plan(ctx, LinkReq{Manga: m.ID, Links: library.Links{metadata.IDCodeAnilist: "30002"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Changes.Apply(ctx, LinkReq{Manga: m.ID, Links: library.Links{metadata.IDCodeAnilist: "30002"}}, ApplyOpts{}); err != nil {
		t.Fatal(err)
	}
	v, err = c.Changes.Variant(ctx, p, BackupsReq{Mangas: []int64{m.ID}, ReplaceAll: true})
	if err != nil {
		t.Fatal(err)
	}
	var changed *PlanChangedError
	if _, err := c.Changes.ApplyPlan(ctx, v, ApplyOpts{}); !errors.As(err, &changed) {
		t.Errorf("stale variant applied: %v", err)
	}
}

func TestDismissPick(t *testing.T) {
	c, _ := testLinkCore(t)
	ctx := context.Background()
	m := addManga(t, c, filepath.Join("MangaDex", "Berserk"), "mango-mangadex", "b", "Berserk", 1)
	req := DismissPickReq{Manga: m.ID, Source: metadata.IDCodeKitsu}
	if p, err := c.Changes.Plan(ctx, req); err != nil || !p.Nothing {
		t.Fatalf("no pick: %+v %v", p, err)
	}
	lib := c.env.Storage.Library()
	if _, err := lib.SetLinkPick(storage.LinkPick{Manga: m.ID, Source: metadata.IDCodeKitsu, AniList: "30002",
		Candidates: []storage.LinkCandidate{{ID: "1", From: "mango-mangadex"}, {ID: "2", From: storage.FromKitsu}}}); err != nil {
		t.Fatal(err)
	}
	p, err := c.Changes.Plan(ctx, req)
	if err != nil || p.Nothing || !strings.Contains(p.Lines[0], "1 or 2") {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := c.Changes.ApplyPlan(ctx, p, ApplyOpts{}); err != nil {
		t.Fatal(err)
	}
	if pick, ok, _ := c.Library.LinkPick(ctx, m.ID, metadata.IDCodeKitsu); !ok || !pick.Dismissed {
		t.Errorf("not dismissed: %+v", pick)
	}
	if info, err := c.Library.Links(ctx, m.ID); err != nil || info.Manga.ID != m.ID {
		t.Errorf("links: %+v %v", info, err)
	}
}

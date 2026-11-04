package core

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/spf13/afero"
)

// replacing or removing a binding is an undoable operation, a new one doesn't need it
func TestBind(t *testing.T) {
	c, _ := testCore(t)
	env := c.env
	env.BindMetadata = func(_ context.Context, _ metadata.IDCode, id string) (metadata.Metadata, error) {
		if id == "404" {
			return nil, errors.New("not found")
		}
		return testMeta("A", id, ""), nil
	}
	c, _ = New(context.Background(), env)
	ctx := context.Background()
	bind := func(id string) OpResult {
		t.Helper()
		res, err := c.Changes.Apply(ctx, BindReq{Provider: "p", MangaID: "m", Code: metadata.IDCodeAnilist, ID: id}, ApplyOpts{})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := bind("1"); res.OpID != 0 || res.Detail.(metadata.Metadata).ID().Raw != "1" {
		t.Fatalf("new binding: %+v", res)
	}
	if res := bind("1"); res.OpID != 0 {
		t.Fatalf("same binding: %+v", res)
	}
	if res := bind("2"); res.OpID == 0 || res.Summary != "bind p m to AniList 2 (was 1)" {
		t.Fatalf("overwrite: %+v", res)
	}
	if _, err := c.Changes.Plan(ctx, BindReq{Provider: "p", MangaID: "m", Code: metadata.IDCodeAnilist, ID: "404"}); err == nil {
		t.Error("bound to a manga that isn't there")
	}
	res, err := c.Changes.Apply(ctx, UnbindReq{Provider: "p", MangaID: "m", Code: metadata.IDCodeAnilist}, ApplyOpts{})
	if err != nil || res.OpID == 0 {
		t.Fatalf("unbind: %+v %v", res, err)
	}
	if _, err := c.Changes.Apply(ctx, UnbindReq{Provider: "p", MangaID: "m", Code: metadata.IDCodeAnilist}, ApplyOpts{}); !errors.Is(err, ErrNothingToDo) {
		t.Errorf("unbind again: %v", err)
	}
	undoLast(t, c)
	if id, _ := c.env.Storage.Bindings().Get("p", "m"); len(id) != 1 || id[0].ID != "2" {
		t.Errorf("unbind not undone: %+v", id)
	}
}

// a binding change gives the folder's .libmangal.json the ids, undone with it
func TestBindMarker(t *testing.T) {
	c, root := testCore(t)
	env := c.env
	env.BindMetadata = func(_ context.Context, _ metadata.IDCode, id string) (metadata.Metadata, error) {
		return testMeta("A", id, ""), nil
	}
	c, _ = New(context.Background(), env)
	ctx := context.Background()
	m := addManga(t, c, "Berserk", "p", "b", "Berserk", 1)
	al := metadata.ID{Raw: "1", Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}
	if err := libmangal.WriteMangaDirInfo(afero.NewOsFs(), m.Path(), libmangal.MangaDirInfo{Provider: "p", Manga: mangadata.MangaInfo{ID: "b"}, MetadataIDs: []metadata.ID{al}}, 0o644); err != nil {
		t.Fatal(err)
	}
	ids := func() []metadata.ID {
		t.Helper()
		info, err := libmangal.ReadMangaDirInfo(afero.NewOsFs(), filepath.Join(root, "Berserk"))
		if err != nil {
			t.Fatal(err)
		}
		return info.MetadataIDs
	}
	kitsu := metadata.ID{Raw: "5", Source: metadata.IDSourceKitsu, Code: metadata.IDCodeKitsu}
	res, err := c.Changes.Apply(ctx, BindReq{Provider: "p", MangaID: "b", Code: metadata.IDCodeKitsu, ID: "5"}, ApplyOpts{})
	if err != nil || res.OpID == 0 {
		t.Fatalf("new binding: %+v %v", res, err)
	}
	if got := ids(); !slices.Equal(got, []metadata.ID{al, kitsu}) {
		t.Errorf("marker ids %+v", got)
	}
	// nothing changes, nothing journaled
	if res, _ := c.Changes.Apply(ctx, BindReq{Provider: "p", MangaID: "b", Code: metadata.IDCodeKitsu, ID: "5"}, ApplyOpts{}); res.OpID != 0 {
		t.Errorf("same binding journaled: %+v", res)
	}
	if _, err := c.Changes.Apply(ctx, BindReq{Provider: "p", MangaID: "b", Code: metadata.IDCodeKitsu, ID: "6"}, ApplyOpts{}); err != nil {
		t.Fatal(err)
	}
	if got := ids(); !slices.Contains(got, metadata.ID{Raw: "6", Source: metadata.IDSourceKitsu, Code: metadata.IDCodeKitsu}) || slices.Contains(got, kitsu) {
		t.Errorf("replaced binding, marker ids %+v", got)
	}
	undoLast(t, c)
	if got := ids(); !slices.Equal(got, []metadata.ID{al, kitsu}) {
		t.Errorf("undone, marker ids %+v", got)
	}
	// the new binding goes with its marker change
	undoLast(t, c)
	if got := ids(); !slices.Equal(got, []metadata.ID{al}) {
		t.Errorf("new binding undone, marker ids %+v", got)
	}
	if b, _ := c.env.Storage.Bindings().Get("p", "b"); len(b) != 0 {
		t.Errorf("new binding undone, bindings %+v", b)
	}
}

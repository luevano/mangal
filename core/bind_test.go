package core

import (
	"context"
	"errors"
	"testing"

	"github.com/luevano/libmangal/metadata"
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

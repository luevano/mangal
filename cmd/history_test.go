package cmd

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
)

// history clear is an operation: the undo brings the reads back
func TestHistoryClearUndo(t *testing.T) {
	history, err := cache.History()
	if err != nil {
		t.Fatal(err)
	}
	if err := history.Record(storage.Read{Provider: "p", MangaID: "m", MangaTitle: "M", Chapter: 1, Last: time.Now()}); err != nil {
		t.Fatal(err)
	}
	historyClearArgs.Yes = true
	t.Cleanup(func() { historyClearArgs.Yes = false })
	historyClearCmd.SetOut(io.Discard)
	historyClearCmd.SetErr(io.Discard)
	if err := historyClearCmd.RunE(historyClearCmd, []string{"p"}); err != nil {
		t.Fatal(err)
	}
	if reads, _ := history.List("p", ""); len(reads) != 0 {
		t.Fatalf("not cleared: %+v", reads)
	}

	c, err := newCore()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := c.Changes.Plan(context.Background(), core.UndoReq{})
	if err != nil || plan.Detail.(library.UndoPlan).Op.Kind != "history-clear" {
		t.Fatalf("plan %+v, %v", plan, err)
	}
	if _, err := c.Changes.ApplyPlan(context.Background(), plan, core.ApplyOpts{}); err != nil {
		t.Fatal(err)
	}
	if reads, _ := history.List("p", ""); len(reads) != 1 {
		t.Errorf("not back: %+v", reads)
	}
}

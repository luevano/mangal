package core

import (
	"context"
	"fmt"

	"github.com/luevano/libmangal/mangadata"
	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/library"
)

// BindReq binds a provider's manga to a metadata source's id (checked first). A binding
// that replaces another is an undoable operation; a new one isn't (nothing is lost). When
// it's the manga's default binding, its metadata is set on Manga (may be nil).
type BindReq struct {
	Provider string          `json:"provider"`
	MangaID  string          `json:"manga_id"`
	Code     lmmeta.IDCode   `json:"source"`
	ID       string          `json:"id"`
	Manga    mangadata.Manga `json:"-"`
}

func (BindReq) Kind() Kind { return KindBind }

func (r BindReq) plan(ctx context.Context, c *Core) (*prepared, error) {
	if r.Provider == "" || r.MangaID == "" {
		return nil, invalid("a binding needs the provider and the manga id")
	}
	meta, err := c.env.BindMetadata(ctx, r.Code, r.ID)
	if err != nil {
		return nil, err
	}
	bindings := c.env.Storage.Bindings()
	previous, err := client.BoundID(bindings, r.Provider, r.MangaID, r.Code)
	if err != nil {
		return nil, err
	}
	name := client.SourceName(r.Code)
	summary := fmt.Sprintf("bind %s %s to %s %s", r.Provider, r.MangaID, name, r.ID)
	if previous != "" && previous != r.ID {
		summary += fmt.Sprintf(" (was %s)", previous)
	}
	set := func() error { return client.SetBound(bindings, r.Provider, r.MangaID, r.Manga, r.Code, r.ID, meta) }
	return &prepared{
		// the same binding again still sets the metadata on the manga
		plan:   Plan{Summary: summary, Lines: []string{fmt.Sprintf("%s: %s", summary, meta)}, Detail: meta},
		tables: []string{"metadata_bindings"},
		apply: func(ctx context.Context, _ ApplyOpts, res *OpResult) error {
			res.Detail = meta
			if previous == "" || previous == r.ID {
				return set()
			}
			return c.journaled(ctx, "bind", summary, res, func(library.Journal) error { return set() })
		},
	}, nil
}

// UnbindReq removes a provider manga's binding of a source, as an undoable operation.
type UnbindReq struct {
	Provider string        `json:"provider"`
	MangaID  string        `json:"manga_id"`
	Code     lmmeta.IDCode `json:"source"`
}

func (UnbindReq) Kind() Kind { return KindUnbind }

func (r UnbindReq) plan(_ context.Context, c *Core) (*prepared, error) {
	bindings := c.env.Storage.Bindings()
	previous, err := client.BoundID(bindings, r.Provider, r.MangaID, r.Code)
	if err != nil {
		return nil, err
	}
	name := client.SourceName(r.Code)
	if previous == "" {
		return &prepared{plan: Plan{Lines: []string{fmt.Sprintf("%s %s isn't bound to %s", r.Provider, r.MangaID, name)}, Nothing: true}, tables: []string{"metadata_bindings"}}, nil
	}
	summary := fmt.Sprintf("unbind %s %s from %s %s", r.Provider, r.MangaID, name, previous)
	return &prepared{
		plan:   Plan{Summary: summary, Lines: []string{summary}},
		tables: []string{"metadata_bindings"},
		apply: func(ctx context.Context, _ ApplyOpts, res *OpResult) error {
			return c.journaled(ctx, "bind", summary, res, func(library.Journal) error {
				_, err := bindings.Remove(r.Provider, r.MangaID, r.Code)
				return err
			})
		},
	}, nil
}

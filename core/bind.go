package core

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
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
	marked, err := c.markedSource(r.Provider, r.MangaID)
	if err != nil {
		return nil, err
	}
	lines := []string{fmt.Sprintf("%s: %s", summary, meta)}
	if marked != nil {
		lines = append(lines, markerLine(*marked))
	}
	return &prepared{
		// the same binding again still sets the metadata on the manga
		plan:   Plan{Summary: summary, Lines: lines, Detail: meta},
		tables: []string{"metadata_bindings"},
		apply: func(ctx context.Context, _ ApplyOpts, res *OpResult) error {
			res.Detail = meta
			// a new binding loses nothing, unless it rewrites the .libmangal.json
			journal := previous != "" && previous != r.ID || marked != nil && previous == ""
			if previous == r.ID && marked != nil {
				// the same binding again: only the marker may lack its ids
				_, changes, err := c.files().SyncMarker(*marked, true)
				if err != nil {
					return err
				}
				journal = changes
			}
			if !journal {
				return set()
			}
			return c.journaled(ctx, "bind", summary, res, func(j library.Journal) error {
				if err := set(); err != nil {
					return err
				}
				return c.syncMarker(j, marked)
			})
		},
	}, nil
}

// markedSource is the library manga the provider manga is a source of, nil unless its
// folder has a .libmangal.json: a binding change gives it the ids (library.Files.SyncMarker).
func (c *Core) markedSource(provider, mangaID string) (*storage.Manga, error) {
	m, ok, err := c.env.Storage.Library().MangaBySource(provider, mangaID)
	if err != nil || !ok || m.Dir == "" {
		return nil, err
	}
	if _, err := libmangal.ReadMangaDirInfo(c.env.FS, m.Path()); err != nil {
		return nil, nil
	}
	return &m, nil
}

func markerLine(m storage.Manga) string {
	return fmt.Sprintf("  %s gets the metadata ids the library knows", filepath.Join(m.Path(), libmangal.FilenameMangaDirInfo))
}

// syncMarker gives the manga's .libmangal.json (if any) the ids the library knows now.
func (c *Core) syncMarker(j library.Journal, m *storage.Manga) error {
	if m == nil {
		return nil
	}
	files := c.files()
	files.Journal = j
	_, _, err := files.SyncMarker(*m, false)
	return err
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
	marked, err := c.markedSource(r.Provider, r.MangaID)
	if err != nil {
		return nil, err
	}
	lines := []string{summary}
	if marked != nil {
		lines = append(lines, markerLine(*marked))
	}
	return &prepared{
		plan:   Plan{Summary: summary, Lines: lines},
		tables: []string{"metadata_bindings"},
		apply: func(ctx context.Context, _ ApplyOpts, res *OpResult) error {
			return c.journaled(ctx, "bind", summary, res, func(j library.Journal) error {
				if _, err := bindings.Remove(r.Provider, r.MangaID, r.Code); err != nil {
					return err
				}
				return c.syncMarker(j, marked)
			})
		},
	}, nil
}

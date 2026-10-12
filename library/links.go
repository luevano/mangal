package library

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

// LinkSources are the metadata sources a library manga links to, by priority.
var LinkSources = []metadata.IDCode{metadata.IDCodeAnilist, metadata.IDCodeKitsu, metadata.IDCodeMyAnimeList}

// Links are metadata ids by source; "" is none (removes it in a change).
type Links map[metadata.IDCode]string

// LinkSource is what tells a library manga's id of a metadata source.
type LinkSource struct {
	Code metadata.IDCode
	// Library are the manga's own ids of the source (metadata_ids, which only grew before
	// links replaced them, so there may be several).
	Library []string
	// Bindings are its sources' bindings of the source, the current source's first.
	Bindings []storage.Binding
	// Marker is the folder's .libmangal.json id, Tag the folder name's ("[alid-N]").
	Marker, Tag string
}

// Current is the id the manga has: its current source's binding, else its own (the last
// added), else the .libmangal.json's, else the folder tag's. Bindings and its own ids win
// over the folder: they're what a bind sets.
func (s LinkSource) Current() string {
	switch {
	case len(s.Bindings) != 0:
		return s.Bindings[0].ID
	case len(s.Library) != 0:
		return s.Library[len(s.Library)-1]
	case s.Marker != "":
		return s.Marker
	}
	return s.Tag
}

// Linked is true when a bind (or a download) set it: a binding or its own id.
func (s LinkSource) Linked() bool { return len(s.Bindings) != 0 || len(s.Library) != 0 }

// Suggested is the folder's id (tag, then .libmangal.json) when nothing links it or it
// differs, empty otherwise.
func (s LinkSource) Suggested() string {
	cur := s.Current()
	for _, id := range []string{s.Tag, s.Marker} {
		if id != "" && (!s.Linked() || id != cur) {
			return id
		}
	}
	return ""
}

// Disagreements are the places that have another id than Current, as "folder tag 1".
func (s LinkSource) Disagreements() []string {
	cur := s.Current()
	var out []string
	for _, b := range s.Bindings {
		if b.ID != cur {
			out = append(out, fmt.Sprintf("%s binding %s", b.Provider, b.ID))
		}
	}
	for _, id := range s.Library {
		if id != cur {
			out = append(out, "library "+id)
		}
	}
	if s.Marker != "" && s.Marker != cur {
		out = append(out, libmangal.FilenameMangaDirInfo+" "+s.Marker)
	}
	if s.Tag != "" && s.Tag != cur {
		out = append(out, "folder tag "+s.Tag)
	}
	return out
}

// Where says where Current comes from and what disagrees, as "binding, library;
// disagree: folder tag 1".
func (s LinkSource) Where() string {
	cur := s.Current()
	var from []string
	if slices.ContainsFunc(s.Bindings, func(b storage.Binding) bool { return b.ID == cur }) {
		from = append(from, "binding")
	}
	if slices.Contains(s.Library, cur) {
		from = append(from, "library")
	}
	if s.Marker == cur && cur != "" {
		from = append(from, libmangal.FilenameMangaDirInfo)
	}
	if s.Tag == cur && cur != "" {
		from = append(from, "folder tag")
	}
	where := strings.Join(from, ", ")
	if d := s.Disagreements(); len(d) != 0 {
		where += "; disagree: " + strings.Join(d, ", ")
	}
	return where
}

// LinkInfo is what tells a library manga's metadata links.
type LinkInfo struct {
	Manga   storage.Manga
	Sources []LinkSource
	// Marker is the folder's .libmangal.json, nil if it has none (or no folder).
	Marker *libmangal.MangaDirInfo
}

// Source is the LinkSource of the code.
func (i LinkInfo) Source(code metadata.IDCode) LinkSource {
	for _, s := range i.Sources {
		if s.Code == code {
			return s
		}
	}
	return LinkSource{Code: code}
}

// Current are the ids the manga has (LinkSource.Current), by source.
func (i LinkInfo) Current() Links {
	links := Links{}
	for _, s := range i.Sources {
		if id := s.Current(); id != "" {
			links[s.Code] = id
		}
	}
	return links
}

// LinkInfo reads what tells the manga's links: its ids, its sources' bindings, its
// .libmangal.json and its folder's name.
func (f Files) LinkInfo(manga storage.Manga) (LinkInfo, error) {
	bindings := storage.Bindings{DB: f.Library.DB}
	var bound []storage.Binding
	for _, src := range manga.Sources {
		list, err := bindings.Get(src.Provider, src.MangaID)
		if err != nil {
			return LinkInfo{Manga: manga}, err
		}
		bound = append(bound, list...)
	}
	var marker *libmangal.MangaDirInfo
	if manga.Dir != "" {
		info, err := libmangal.ReadMangaDirInfo(f.FS, manga.Path())
		switch {
		case err == nil:
			marker = &info
		case !errors.Is(err, fs.ErrNotExist):
			// an invalid one is like none, as for scans
		}
	}
	return LinkInfoOf(manga, bound, marker), nil
}

// LinkInfoOf is the manga's LinkInfo from its sources' bindings (the current source's
// first) and its .libmangal.json (nil for none); the folder tags come from its name.
func LinkInfoOf(manga storage.Manga, bound []storage.Binding, marker *libmangal.MangaDirInfo) LinkInfo {
	info := LinkInfo{Manga: manga, Marker: marker}
	var tags []metadata.ID
	if manga.Dir != "" {
		tags = tagIDs(manga.Dir)
	}
	for _, code := range LinkSources {
		s := LinkSource{Code: code}
		for _, id := range manga.MetadataIDs {
			if id.Code == code && !slices.Contains(s.Library, id.Raw) {
				s.Library = append(s.Library, id.Raw)
			}
		}
		for _, b := range bound {
			if b.Source == code {
				s.Bindings = append(s.Bindings, b)
			}
		}
		if marker != nil {
			s.Marker = idOf(marker.MetadataIDs, code)
		}
		s.Tag = idOf(tags, code)
		info.Sources = append(info.Sources, s)
	}
	return info
}

func idOf(ids []metadata.ID, code metadata.IDCode) string {
	for _, id := range ids {
		if id.Code == code {
			return id.Raw
		}
	}
	return ""
}

// tagIDs are the ids of a folder name's tags ("[alid-N]").
func tagIDs(name string) []metadata.ID {
	var ids []metadata.ID
	for _, m := range idTag.FindAllStringSubmatch(name, -1) {
		code := metadata.IDCode(m[1])
		ids = append(ids, metadata.ID{Raw: m[2], Source: idSources[code], Code: code})
	}
	return ids
}

// Linker changes the metadata links of library mangas.
type Linker struct {
	Files
	Loaders []libmangal.ProviderLoader
	// MangaName is the manga name template, Metadata looks up the AniList metadata of ids
	// (nil if none): together they tell the folder name a link implies.
	MangaName func(libmangal.ProviderInfo, mangadata.Manga) string
	Metadata  func(ctx context.Context, ids []metadata.ID) (metadata.Metadata, error)
}

// LinkChange is a source's id changing.
type LinkChange struct {
	Code metadata.IDCode
	// Old are the ids it had (its own, bindings, .libmangal.json), New the one it gets ("" removes).
	Old []string
	New string
}

// LinkWatch is a watch of the manga, not one of its sources, bound to another id.
type LinkWatch struct {
	Watch     storage.Watch
	Code      metadata.IDCode
	Has, Want string
}

// LinkOther is another library manga with the id: they're the same manga, to merge.
type LinkOther struct {
	Manga storage.Manga
	Code  metadata.IDCode
	ID    string
}

// LinkPlan is what changing a library manga's links does: its own ids, its sources'
// bindings and its .libmangal.json get the new ids, the old ones go. Rename, Watches and
// Others are changes it implies, done only when asked (LinkOptions) or not at all.
type LinkPlan struct {
	Info LinkInfo
	// Links are the asked links, Changes what really changes.
	Links   Links
	Changes []LinkChange
	// Marker is the folder's new .libmangal.json, nil when it has none or it doesn't change.
	Marker *libmangal.MangaDirInfo
	// Bindings are the provider mangas whose bindings change (its sources), Watched the
	// watch of each (0 if none).
	Bindings [][2]string
	Watched  []int64
	Root     string

	// Rename is the folder's rename to the name the template gives with the links, nil if
	// it keeps its name; RenameNote why it wasn't worked out, if it wasn't.
	Rename     *Rename
	RenameNote string
	Watches    []LinkWatch
	Others     []LinkOther
}

// Manga is the plan's library manga.
func (p LinkPlan) Manga() storage.Manga { return p.Info.Manga }

// Implied is true when the plan implies changes to ask about.
func (p LinkPlan) Implied() bool {
	return p.Rename != nil && p.Rename.Skip == "" || len(p.Watches) != 0
}

// LinkOptions are the implied changes ApplyLinks also makes.
type LinkOptions struct {
	Rename, Watches bool
}

// PlanOptions say what PlanLinks works out.
type PlanOptions struct {
	// NoImplied skips the implied changes (the rename's metadata lookup, watches, others).
	NoImplied bool
}

// PlanLinks plans giving the manga the links (the sources not in it keep theirs).
func (l Linker) PlanLinks(ctx context.Context, manga storage.Manga, links Links, opts PlanOptions) (LinkPlan, error) {
	info, err := l.LinkInfo(manga)
	if err != nil {
		return LinkPlan{}, fmt.Errorf("links of %q: %w", manga.Title, err)
	}
	plan := LinkPlan{Info: info, Links: links, Root: l.Root}
	for _, src := range manga.Sources {
		plan.Bindings = append(plan.Bindings, [2]string{src.Provider, src.MangaID})
	}
	for _, code := range LinkSources {
		id, ok := links[code]
		if !ok {
			continue
		}
		s := info.Source(code)
		var old []string
		add := func(o string) {
			if o != "" && !slices.Contains(old, o) {
				old = append(old, o)
			}
		}
		for _, o := range s.Library {
			add(o)
		}
		for _, b := range s.Bindings {
			add(b.ID)
		}
		add(s.Marker)
		// its own id, every source's binding and the marker's (when it has one) are it already
		var has bool
		if id == "" {
			has = len(old) == 0
		} else {
			has = slices.Equal(s.Library, []string{id}) && (info.Marker == nil || s.Marker == id) &&
				len(s.Bindings) == len(manga.Sources) && !slices.ContainsFunc(s.Bindings, func(b storage.Binding) bool { return b.ID != id })
		}
		if !has {
			plan.Changes = append(plan.Changes, LinkChange{Code: code, Old: old, New: id})
		}
	}
	if info.Marker != nil {
		ids := storage.ReplaceIDs(info.Marker.MetadataIDs, links)
		if !sameIDs(ids, info.Marker.MetadataIDs) {
			marker := *info.Marker
			marker.MetadataIDs = ids
			plan.Marker = &marker
		}
	}
	if opts.NoImplied {
		return plan, nil
	}
	final := info.Current()
	for code, id := range links {
		if id == "" {
			delete(final, code)
		} else {
			final[code] = id
		}
	}
	if err := l.planRename(ctx, &plan, final); err != nil {
		return plan, fmt.Errorf("links of %q: %w", manga.Title, err)
	}
	if err := l.planWatches(&plan, final); err != nil {
		return plan, fmt.Errorf("links of %q: %w", manga.Title, err)
	}
	if err := l.planOthers(&plan, links); err != nil {
		return plan, fmt.Errorf("links of %q: %w", manga.Title, err)
	}
	return plan, nil
}

func sameIDs(a, b []metadata.ID) bool {
	return slices.EqualFunc(a, b, func(x, y metadata.ID) bool { return x.Code == y.Code && x.Raw == y.Raw })
}

// planRename works out the folder name the template gives with the AniList link.
func (l Linker) planRename(ctx context.Context, plan *LinkPlan, final Links) error {
	m := plan.Manga()
	switch {
	case m.Dir == "":
		return nil
	case l.MangaName == nil || l.Metadata == nil:
		return nil
	case final[metadata.IDCodeAnilist] == "":
		plan.RenameNote = "no AniList link, the folder name isn't checked"
		return nil
	}
	id, _ := storage.Binding{Source: metadata.IDCodeAnilist, ID: final[metadata.IDCodeAnilist]}.MetadataID()
	report(l.OnProgress, Progress{Phase: "looking up metadata"})
	meta, err := l.Metadata(ctx, []metadata.ID{id})
	if err != nil {
		return fmt.Errorf("metadata: %w", err)
	}
	if metadata.Validate(meta) != nil {
		plan.RenameNote = fmt.Sprintf("no AniList metadata for %s, the folder name isn't checked", id.Raw)
		return nil
	}
	provider, info := l.folderManga(plan.Info)
	name := l.MangaName(provider, &dirManga{info: info, meta: meta})
	if name == "" || name == m.Dir {
		return nil
	}
	r := Rename{From: m.Path(), To: filepath.Join(m.Parent, name)}
	if exists, err := afero.Exists(l.FS, r.To); err != nil {
		return err
	} else if exists && !sameFile(l.FS, r.From, r.To) {
		r.Skip = "a folder of that name exists"
	}
	plan.Rename = &r
	return nil
}

// folderManga is the provider and manga the folder's name comes from: its
// .libmangal.json's, else its current source's, else its provider directory and title.
func (l Linker) folderManga(info LinkInfo) (libmangal.ProviderInfo, mangadata.MangaInfo) {
	m := info.Manga
	provider := libmangal.ProviderInfo{ID: filepath.Base(m.Parent), Name: filepath.Base(m.Parent)}
	manga := mangadata.MangaInfo{Title: m.Title}
	switch {
	case info.Marker != nil && info.Marker.Provider != "":
		provider = libmangal.ProviderInfo{ID: info.Marker.Provider, Name: info.Marker.Provider}
		manga = info.Marker.Manga
	case len(m.Sources) != 0:
		provider = libmangal.ProviderInfo{ID: m.Sources[0].Provider, Name: m.Sources[0].Provider}
		manga = mangadata.MangaInfo{ID: m.Sources[0].MangaID, Title: cmpOr(m.Sources[0].Title, m.Title)}
	}
	if loader, _, ok := libmangal.FindLoader(l.Loaders, provider.ID); ok {
		provider = loader.Info()
	}
	return provider, manga
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// planWatches lists the manga's watches (not of its sources, those follow) bound to other
// ids than the links.
func (l Linker) planWatches(plan *LinkPlan, final Links) error {
	watches := storage.Watches{DB: l.Library.DB}
	list, err := watches.List()
	if err != nil {
		return err
	}
	bindings := storage.Bindings{DB: l.Library.DB}
	m := plan.Manga()
	plan.Watched = make([]int64, len(plan.Bindings))
	for _, w := range list {
		if i := slices.Index(plan.Bindings, [2]string{w.Provider, w.MangaID}); i >= 0 {
			// its binding follows, as the source's
			plan.Watched[i] = w.ID
			continue
		}
		if w.LibraryManga != m.ID {
			continue
		}
		bound, err := bindings.Get(w.Provider, w.MangaID)
		if err != nil {
			return err
		}
		for _, code := range LinkSources {
			if _, asked := plan.Links[code]; !asked {
				continue
			}
			want, has := final[code], ""
			for _, b := range bound {
				if b.Source == code {
					has = b.ID
				}
			}
			if has != want {
				plan.Watches = append(plan.Watches, LinkWatch{Watch: w, Code: code, Has: has, Want: want})
			}
		}
	}
	return nil
}

// planOthers lists the other library mangas with one of the new ids.
func (l Linker) planOthers(plan *LinkPlan, links Links) error {
	mangas, err := l.Library.Mangas()
	if err != nil {
		return err
	}
	bindings, err := storage.Bindings{DB: l.Library.DB}.List("", "")
	if err != nil {
		return err
	}
	bySource := map[[2]string][]storage.Binding{}
	for _, b := range bindings {
		bySource[[2]string{b.Provider, b.MangaID}] = append(bySource[[2]string{b.Provider, b.MangaID}], b)
	}
	for _, other := range mangas {
		if other.ID == plan.Manga().ID {
			continue
		}
		for _, code := range LinkSources {
			id := links[code]
			if id == "" {
				continue
			}
			has := slices.ContainsFunc(other.MetadataIDs, func(mid metadata.ID) bool { return mid.Code == code && mid.Raw == id })
			for _, src := range other.Sources {
				has = has || slices.ContainsFunc(bySource[[2]string{src.Provider, src.MangaID}], func(b storage.Binding) bool { return b.Source == code && b.ID == id })
			}
			if has {
				plan.Others = append(plan.Others, LinkOther{Manga: other, Code: code, ID: id})
				break
			}
		}
	}
	return nil
}

// sourceName is the name of a link source.
func sourceName(code metadata.IDCode) string {
	if name, ok := duplicateNames[code]; ok {
		return name
	}
	return string(code)
}

// Lines describe the changes, one per line.
func (p LinkPlan) Lines() []string {
	m := p.Manga()
	lines := []string{fmt.Sprintf("link %q (library manga %d):", m.Title, m.ID)}
	if len(p.Changes) == 0 {
		lines = append(lines, "  has these links already")
	}
	for _, c := range p.Changes {
		var was []string
		for _, o := range c.Old {
			if o != c.New {
				was = append(was, o)
			}
		}
		old := "none"
		if len(was) != 0 {
			old = strings.Join(was, ", ")
		}
		switch c.New {
		case "":
			lines = append(lines, fmt.Sprintf("  %s: remove %s", sourceName(c.Code), old))
		default:
			lines = append(lines, fmt.Sprintf("  %s: %s -> %s", sourceName(c.Code), old, c.New))
		}
	}
	if len(p.Changes) != 0 {
		var where []string
		if len(p.Bindings) != 0 {
			var b []string
			for i, k := range p.Bindings {
				if i < len(p.Watched) && p.Watched[i] != 0 {
					b = append(b, fmt.Sprintf("%s %s (watch %d)", k[0], k[1], p.Watched[i]))
				} else {
					b = append(b, k[0]+" "+k[1])
				}
			}
			where = append(where, "bindings of "+strings.Join(b, ", "))
		}
		where = append(where, "library")
		if p.Marker != nil {
			where = append(where, libmangal.FilenameMangaDirInfo)
		} else if p.Info.Marker == nil && m.Dir != "" {
			where = append(where, "no "+libmangal.FilenameMangaDirInfo+" to update (mangal library scan --mark writes one)")
		}
		lines = append(lines, "  in: "+strings.Join(where, ", "))
	}
	return lines
}

// ImpliedLines describe the implied changes and how to make them: the flags (CLI) or
// keys of each, e.g. map[string]string{"rename": "--rename", "watches": "--watches"}.
func (p LinkPlan) ImpliedLines(how map[string]string) []string {
	rel := func(path string) string {
		if r, err := filepath.Rel(p.Root, path); err == nil && p.Root != "" && isInside(p.Root, path) {
			return r
		}
		return path
	}
	var lines []string
	if r := p.Rename; r != nil {
		lines = append(lines, "folder name under the template:", "  from "+rel(r.From), "  to   "+rel(r.To))
		if r.Skip != "" {
			lines = append(lines, "  can't rename: "+r.Skip)
		} else if h := how["rename"]; h != "" {
			lines = append(lines, "  "+h+" renames it")
		}
	}
	for _, w := range p.Watches {
		has := w.Has
		if has == "" {
			has = "none"
		}
		want := w.Want
		if want == "" {
			want = "none"
		}
		line := fmt.Sprintf("watch %d (%s %s) is bound to %s %s, not %s", w.Watch.ID, w.Watch.Provider, w.Watch.MangaID, sourceName(w.Code), has, want)
		if h := how["watches"]; h != "" {
			line += ": " + h + " updates it"
		}
		lines = append(lines, line)
	}
	for _, o := range p.Others {
		keep, other := o.Manga, p.Manga()
		if compareKeep(DuplicateManga{Manga: other}, DuplicateManga{Manga: keep}) < 0 {
			keep, other = other, keep
		}
		lines = append(lines, fmt.Sprintf("library manga %d %q has %s %s too, merge them: mangal library merge %d %d",
			o.Manga.ID, o.Manga.Title, sourceName(o.Code), o.ID, keep.ID, other.ID))
	}
	return lines
}

// ApplyLinks does the plan (and the implied changes opts asks for): the .libmangal.json
// first, then the rename, then the library and the bindings in one transaction.
func (f Files) ApplyLinks(plan LinkPlan, opts LinkOptions) error {
	m := plan.Manga()
	fail := func(err error) error { return fmt.Errorf("link %q: %w", m.Title, err) }
	if plan.Marker != nil {
		if err := writeDirInfo(f.FS, f.Journal, m.Path(), *plan.Marker, f.Modes.File); err != nil {
			return fail(fmt.Errorf("write %s: %w", libmangal.FilenameMangaDirInfo, err))
		}
	}
	change := storage.LinkChange{Manga: m.ID, Links: map[metadata.IDCode]string{}, Bindings: slices.Clone(plan.Bindings), At: time.Now()}
	for code, id := range plan.Links {
		change.Links[code] = id
	}
	if opts.Watches {
		for _, w := range plan.Watches {
			if k := [2]string{w.Watch.Provider, w.Watch.MangaID}; !slices.Contains(change.Bindings, k) {
				change.Bindings = append(change.Bindings, k)
			}
		}
	}
	if opts.Rename && plan.Rename != nil && plan.Rename.Skip == "" {
		if err := applyRename(f.FS, f.Journal, *plan.Rename); err != nil {
			return fail(err)
		}
		change.From, change.To = plan.Rename.From, plan.Rename.To
	}
	if err := f.Library.SetLinks(change); err != nil {
		return fail(err)
	}
	return nil
}

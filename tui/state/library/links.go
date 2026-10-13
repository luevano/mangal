package library

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/confirm"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/model/metadata"
	"github.com/luevano/mangal/v5/tui/state/anilist"
	"github.com/luevano/mangal/v5/tui/util"
)

var _ base.State = (*linksState)(nil)

// linkItem is a metadata source with the library manga's link to it.
type linkItem struct {
	src  client.MetadataSource
	link library.LinkSource
	// meta is the linked manga's metadata, nil if not linked, not loaded (err) or not
	// checked (MyAnimeList without an API client)
	meta lmmeta.Metadata
	err  error
	// pick are the ids to pick from (the backups found two), nil if none
	pick *storage.LinkPick
}

func (i *linkItem) FilterValue() string { return i.src.Name }

func (i *linkItem) Title() string {
	switch {
	case !i.link.Linked():
		return i.src.Name + " · not linked"
	case i.meta != nil:
		return fmt.Sprintf("%s %s · %s", i.src.Name, i.link.Current(), i.meta)
	}
	return fmt.Sprintf("%s %s", i.src.Name, i.link.Current())
}

func (i *linkItem) Description() string {
	var parts []string
	if i.link.Linked() {
		parts = append(parts, i.link.Where())
	}
	if i.pick != nil {
		var ids []string
		for _, c := range i.pick.Candidates {
			ids = append(ids, fmt.Sprintf("%s (%s)", c.ID, c.FromName()))
		}
		parts = append(parts, "pick "+strings.Join(ids, " or ")+" (enter)")
	}
	if sug := i.link.Suggested(); sug != "" {
		parts = append(parts, fmt.Sprintf("the folder says %s (enter searches it)", sug))
	}
	if i.err != nil {
		parts = append(parts, i.err.Error())
	}
	if len(parts) == 0 {
		return "enter searches it"
	}
	return strings.Join(parts, " · ")
}

// linksState is a library manga's metadata links (AniList, Kitsu, MyAnimeList), shared by
// its sources' bindings, its own ids and its .libmangal.json.
type linksState struct {
	list.Screen
	manga   storage.Manga
	confirm *confirm.Dialog
	// autoLinks are planned, the pick of autoPick asked, once the links load (NewLinks,
	// NewLinksPick)
	autoLinks library.Links
	autoPick  lmmeta.IDCode

	search, remove, backups, metadata key.Binding
}

type linksLoadedMsg struct {
	manga storage.Manga
	items []*linkItem
}

// linkPickedMsg is a source's manga picked in its search.
type linkPickedMsg struct {
	code lmmeta.IDCode
	id   string
}

// pickLoadedMsg is a pick with the metadata of its candidates (nil when it didn't load).
type pickLoadedMsg struct {
	pick  storage.LinkPick
	metas []lmmeta.Metadata
}

// linkPlannedMsg is a planned link change, to ask about.
type linkPlannedMsg struct {
	linker library.Linker
	plan   library.LinkPlan
}

// backupsFoundMsg are the backups found, to ask about.
type backupsFoundMsg struct {
	linker  library.Linker
	backups []library.Backup
	summary string
}

// linksCmd opens the links screen of the manga.
func linksCmd(m storage.Manga) tea.Cmd {
	return func() tea.Msg { return newLinksState(m) }
}

func newLinksState(m storage.Manga) *linksState {
	s := &linksState{
		manga:    m,
		confirm:  newDialog(),
		search:   util.Bind("search", "enter", "s"),
		remove:   base.KeyRemove,
		backups:  base.KeyBackups,
		metadata: base.KeyMetadata,
	}
	s.Screen = list.NewScreen(list.New(2, 1, "source", "sources", []*linkItem{}, func(i *linkItem) _list.DefaultItem { return i }), s.keyMap)
	return s
}

func (s *linksState) keyMap() help.KeyMap {
	return util.KeyMap{Short: []key.Binding{s.search, s.remove, s.backups, s.metadata}}
}

func (s *linksState) Key() string { return fmt.Sprintf("library-links/%d", s.manga.ID) }

func (s *linksState) Title() base.Title {
	return base.Title{Text: "Links", Background: color.Anilist, Foreground: color.Bright}
}

func (s *linksState) Subtitle() string {
	return fmt.Sprintf("%s · %s", s.manga.Title, s.manga.Path())
}

func (s *linksState) Backable() bool  { return s.Screen.Backable() && !s.confirm.Active() }
func (s *linksState) Inputting() bool { return s.Screen.Inputting() || s.confirm.Active() }

func (s *linksState) Resize(size base.Size) tea.Cmd {
	s.confirm.Resize(size)
	return s.Screen.Resize(size)
}

func (s *linksState) Init(ctx context.Context) tea.Cmd {
	return s.load(ctx)
}

func (s *linksState) load(ctx context.Context) tea.Cmd {
	id := s.manga.ID
	return base.Load(ctx, "Loading the links", func(ctx context.Context) (linksLoadedMsg, error) {
		linker, err := client.LibraryLinker()
		if err != nil {
			return linksLoadedMsg{}, err
		}
		manga, ok, err := linker.Library.MangaByID(id)
		if err != nil {
			return linksLoadedMsg{}, err
		}
		if !ok {
			return linksLoadedMsg{}, fmt.Errorf("library manga %d is gone", id)
		}
		info, err := linker.LinkInfo(manga)
		if err != nil {
			return linksLoadedMsg{}, err
		}
		var items []*linkItem
		for _, src := range client.MetadataSources {
			_, err := client.MetadataProvider(src.Source)
			link := info.Source(src.Code)
			// a source not enabled or configured shows only when linked
			if err != nil && !link.Linked() {
				continue
			}
			i := &linkItem{src: src, link: link}
			if link.Linked() {
				i.meta, i.err = client.LinkMetadata(ctx, src.Code, link.Current())
			}
			if pick, ok, err := linker.Library.LinkPick(manga.ID, src.Code); err != nil {
				return linksLoadedMsg{}, err
			} else if ok && !pick.Dismissed {
				i.pick = &pick
			}
			items = append(items, i)
		}
		return linksLoadedMsg{manga: manga, items: items}, nil
	}, func(msg linksLoadedMsg) tea.Msg { return msg })
}

func (s *linksState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	if msg, ok := msg.(tea.KeyMsg); ok && s.confirm.Active() {
		return s.confirm.Update(msg)
	}
	switch msg := msg.(type) {
	case linksLoadedMsg:
		s.manga = msg.manga
		items := make([]_list.Item, len(msg.items))
		for i, it := range msg.items {
			items[i] = it
		}
		cmd := s.List.SetItemsKeep(items)
		if links := s.autoLinks; len(links) != 0 {
			s.autoLinks = nil
			return tea.Batch(cmd, planLinks(ctx, s.manga, links))
		}
		if source := s.autoPick; source != "" {
			s.autoPick = ""
			for _, it := range msg.items {
				if it.src.Code == source && it.pick != nil {
					s.List.SelectFunc(func(i _list.Item) bool { return i == _list.Item(it) })
					return tea.Batch(cmd, loadPick(ctx, *it.pick))
				}
			}
			return tea.Batch(cmd, base.Notify("Nothing to pick any more"))
		}
		return cmd
	case base.RestoredMsg:
		// links to plan or a pick to ask (health)
		if r, ok := msg.Request.(*linksState); ok {
			s.autoLinks, s.autoPick = r.autoLinks, r.autoPick
		}
		return s.load(ctx)
	case linkPickedMsg:
		return planLinks(ctx, s.manga, library.Links{msg.code: msg.id})
	case pickLoadedMsg:
		s.askPick(msg)
		return nil
	case linkPlannedMsg:
		return askLinks(s.confirm, s.View(), msg)
	case backupsFoundMsg:
		return askBackups(s.confirm, s.View(), msg)
	case actionMsg:
		return msg.run(ctx)
	case reloadMsg:
		return withNotify(s.load(ctx), msg.notify)
	case tea.KeyMsg:
		if s.List.Filtering() {
			break
		}
		if key.Matches(msg, s.backups) {
			return findBackups(ctx, []storage.Manga{s.manga})
		}
		i, ok := s.List.SelectedItem().(*linkItem)
		if !ok {
			break
		}
		switch {
		case msg.String() == "enter" && i.pick != nil:
			return loadPick(ctx, *i.pick)
		case key.Matches(msg, s.search):
			p, err := client.MetadataProvider(i.src.Source)
			if err != nil {
				return func() tea.Msg { return err }
			}
			query := i.link.Suggested()
			if query == "" {
				query = i.link.Current()
			}
			if query == "" {
				query = s.manga.Title
			}
			code := i.src.Code
			return func() tea.Msg {
				return anilist.NewPick(i.src, p, query, func(meta lmmeta.Metadata) tea.Cmd {
					return func() tea.Msg { return linkPickedMsg{code: code, id: meta.ID().Raw} }
				})
			}
		case key.Matches(msg, s.remove) && i.link.Linked():
			return planLinks(ctx, s.manga, library.Links{i.src.Code: ""})
		case key.Matches(msg, s.metadata) && i.meta != nil:
			return metadata.ShowCmd(i.meta)
		}
	}
	return s.List.Update(msg)
}

func (s *linksState) View() string {
	if s.confirm.Active() {
		return s.confirm.View()
	}
	return s.List.View()
}

// loadPick looks the pick's candidates up (pickLoadedMsg), to ask which to link.
func loadPick(ctx context.Context, pick storage.LinkPick) tea.Cmd {
	if base.Busy(ctx) {
		return nil
	}
	return base.Load(ctx, "Looking the ids up", func(ctx context.Context) (pickLoadedMsg, error) {
		msg := pickLoadedMsg{pick: pick, metas: make([]lmmeta.Metadata, len(pick.Candidates))}
		for i, c := range pick.Candidates {
			// one that doesn't load is still a choice
			msg.metas[i], _ = client.LinkMetadata(ctx, pick.Source, c.ID)
		}
		return msg, nil
	}, func(msg pickLoadedMsg) tea.Msg { return msg })
}

// askPick asks which of the pick's ids to link (then the link's plan and confirm), or to
// dismiss it.
func (s *linksState) askPick(msg pickLoadedMsg) {
	src, _ := client.MetadataSourceOf(msg.pick.Source)
	lines := []string{fmt.Sprintf("Linked to AniList %s, its sources give two %s ids:", msg.pick.AniList, src.Name)}
	var choices []confirm.Choice
	for i, c := range msg.pick.Candidates {
		line := fmt.Sprintf("%d. %s %s (%s)", i+1, src.Name, c.ID, c.FromName())
		if meta := msg.metas[i]; meta != nil {
			line += ": " + meta.String()
		}
		lines = append(lines, line)
		code, id := msg.pick.Source, c.ID
		choices = append(choices, confirm.Choice{Key: fmt.Sprint(i + 1), Label: "link " + c.ID, Cmd: func() tea.Msg { return linkPickedMsg{code: code, id: id} }})
	}
	lib, manga, source := client.LibraryFiles, s.manga, msg.pick.Source
	choices = append(choices, confirm.Choice{Key: "d", Label: "neither (dismiss)", Cmd: action("Dismissing", func() (string, error) {
		files, err := lib()
		if err != nil {
			return "", err
		}
		_, err = files.Library.DismissLinkPick(manga.ID, source)
		return "dismissed, nothing linked", err
	})})
	s.confirm.Choose("Pick "+src.Name, strings.Join(lines, "\n"), s.View(), choices...)
}

// planLinks plans giving the manga the links, then asks (linkPlannedMsg).
func planLinks(ctx context.Context, manga storage.Manga, links library.Links) tea.Cmd {
	if base.Busy(ctx) {
		return nil
	}
	return base.LoadWithStatus(ctx, "Planning the link", func(ctx context.Context, status func(string)) (linkPlannedMsg, error) {
		for code, id := range links {
			if id == "" {
				continue
			}
			if _, err := client.LinkMetadata(ctx, code, id); err != nil {
				return linkPlannedMsg{}, err
			}
		}
		linker, err := client.LibraryLinker()
		if err != nil {
			return linkPlannedMsg{}, err
		}
		linker.OnProgress = func(p library.Progress) { status(p.String()) }
		plan, err := linker.PlanLinks(ctx, manga, links, library.PlanOptions{})
		return linkPlannedMsg{linker: linker, plan: plan}, err
	}, func(msg linkPlannedMsg) tea.Msg { return msg })
}

// askLinks asks to make the planned change, with a choice for each implied one (the
// folder rename, the watches' bindings). Another manga with the id is only told.
func askLinks(d *confirm.Dialog, behind string, msg linkPlannedMsg) tea.Cmd {
	plan := msg.plan
	if len(plan.Changes) == 0 && !plan.Implied() {
		lines := plan.ImpliedLines(nil)
		if len(lines) == 0 {
			return base.Notify("It has these links already")
		}
		return base.Viewport("Links", strings.Join(append(plan.Lines(), lines...), "\n"), color.Accent)
	}
	message := strings.Join(append(plan.Lines(), plan.ImpliedLines(nil)...), "\n")
	run := func(opts library.LinkOptions) tea.Cmd {
		linker, m := msg.linker, plan.Manga()
		return action("Linking "+m.Title, func() (string, error) {
			return journaled("link", fmt.Sprintf("link %d %q", m.ID, m.Title), "linked "+m.Title, func(j library.Journal) error {
				linker.Journal = j
				return linker.ApplyLinks(plan, opts)
			})
		})
	}
	if !plan.Implied() {
		d.Ask("Link", message, behind, run(library.LinkOptions{}))
		return nil
	}
	var choices []confirm.Choice
	if len(plan.Changes) != 0 {
		choices = append(choices, confirm.Choice{Key: "y", Label: "link only", Cmd: run(library.LinkOptions{})})
	}
	rename := plan.Rename != nil && plan.Rename.Skip == ""
	watches := len(plan.Watches) != 0
	if rename {
		choices = append(choices, confirm.Choice{Key: "r", Label: "and rename the folder", Cmd: run(library.LinkOptions{Rename: true})})
	}
	if watches {
		choices = append(choices, confirm.Choice{Key: "w", Label: "and bind the watches", Cmd: run(library.LinkOptions{Watches: true})})
	}
	if rename && watches {
		choices = append(choices, confirm.Choice{Key: "a", Label: "all of it", Cmd: run(library.LinkOptions{Rename: true, Watches: true})})
	}
	d.Choose("Link", message, behind, choices...)
	return nil
}

// findBackups finds the Kitsu and MyAnimeList links the mangas can get from AniList, then
// asks (backupsFoundMsg).
func findBackups(ctx context.Context, mangas []storage.Manga) tea.Cmd {
	if base.Busy(ctx) {
		return nil
	}
	return base.LoadWithStatus(ctx, "Looking for backup links", func(ctx context.Context, status func(string)) (backupsFoundMsg, error) {
		finder, err := client.LibraryBackups()
		if err != nil {
			return backupsFoundMsg{}, err
		}
		finder.OnProgress = func(p library.Progress) { status(p.String()) }
		backups, err := finder.Find(ctx, mangas)
		if err != nil {
			return backupsFoundMsg{}, err
		}
		linker, err := client.LibraryLinker()
		return backupsFoundMsg{linker: linker, backups: backups, summary: finder.Summary(backups) + "."}, err
	}, func(msg backupsFoundMsg) tea.Msg { return msg })
}

// askBackups asks to fill the missing links, and whether to replace the ones that differ.
func askBackups(d *confirm.Dialog, behind string, msg backupsFoundMsg) tea.Cmd {
	fill, conflicts, _, _ := library.BackupSummary(msg.backups)
	var lines []string
	for _, b := range msg.backups {
		lines = append(lines, b.Lines()...)
	}
	summary := msg.summary
	if fill == 0 && conflicts == 0 {
		return base.Viewport("Backup links", strings.Join(append(lines, "", summary), "\n"), color.Accent)
	}
	message := strings.Join(append(lines, "", summary), "\n")
	run := func(replace bool) tea.Cmd {
		linker, backups := msg.linker, msg.backups
		return func() tea.Msg {
			return actionMsg{label: "Linking", do: func(status func(string)) (reloadMsg, error) {
				n := 0
				notify, err := journaled("link", "backup links", "backup links set", func(j library.Journal) error {
					linker.Journal = j
					for i, b := range backups {
						links := b.Links(replace)
						if len(links) == 0 {
							continue
						}
						status(library.Progress{Phase: "linking", Done: i + 1, Total: len(backups), Item: b.Manga.Title}.String())
						plan, err := linker.PlanLinks(context.Background(), b.Manga, links, library.PlanOptions{NoImplied: true})
						if err != nil {
							return err
						}
						if err := linker.ApplyLinks(plan, library.LinkOptions{}); err != nil {
							return err
						}
						n++
					}
					return nil
				})
				if err == nil {
					notify = fmt.Sprintf("%d mangas: %s", n, notify)
				}
				return reloadMsg{notify: notify}, err
			}}
		}
	}
	if conflicts == 0 {
		d.Ask("Backup links", message, behind, run(false))
		return nil
	}
	choices := []confirm.Choice{{Key: "a", Label: fmt.Sprintf("also replace the %d that differ", conflicts), Cmd: run(true)}}
	if fill != 0 {
		choices = append([]confirm.Choice{{Key: "y", Label: "fill the missing only", Cmd: run(false)}}, choices...)
	}
	d.Choose("Backup links", message, behind, choices...)
	return nil
}

// Package health is the TUI's health screen: the problems of the library and the watches
// (package health), what fixes them, and the fixes it can apply.
package health

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/health"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/accounts"
	"github.com/luevano/mangal/v5/tui/model/confirm"
	"github.com/luevano/mangal/v5/tui/model/list"
	tuilibrary "github.com/luevano/mangal/v5/tui/state/library"
	"github.com/luevano/mangal/v5/tui/state/migrate"
	"github.com/luevano/mangal/v5/tui/state/watches"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/luevano/mangal/v5/util/cache"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

var _ base.State = (*state)(nil)

var severityColors = map[health.Severity]lipgloss.TerminalColor{
	health.Error:   color.Error,
	health.Warning: color.Warning,
	health.Info:    color.Secondary,
}

type item struct {
	s *state
	f health.Finding
}

func (i *item) FilterValue() string {
	return i.f.Subject.String() + " " + i.f.Check + " " + i.f.Message
}

func (i *item) Title() string {
	// with a filter on, the list highlights the matches by rune position: plain then
	subject := i.f.Subject.String()
	if i.s != nil && !i.s.List.Unfiltered() {
		return subject + " " + i.f.Check
	}
	sev := lipgloss.NewStyle().Foreground(severityColors[i.f.Severity])
	return subject + " " + sev.Render(i.f.Check+" "+string(i.f.Severity))
}

func (i *item) Description() string {
	if i.f.Dismissed {
		return i.f.Message + "\n(dismissed: " + i.s.keys.dismiss.Help().Key + " brings it back)"
	}
	return i.f.Message + "\n" + i.s.hint(i.f)
}

// hint is what the keys do on the finding: "enter watch details · x resume".
func (s *state) hint(f health.Finding) string {
	var parts []string
	if what := jumpLabel(f); what != "" {
		parts = append(parts, "enter "+what)
	}
	if what := applyLabel(f); what != "" {
		parts = append(parts, s.keys.apply.Help().Key+" "+what)
	}
	if a := f.Fix.Action; a != nil && a.Kind == health.ActionMerge {
		parts = append(parts, s.keys.merge.Help().Key+" merge")
	}
	if len(parts) == 0 && len(f.Fix.Commands) != 0 {
		parts = append(parts, f.Fix.Commands[0])
	}
	return strings.Join(parts, " · ")
}

// jumpLabel is where enter goes from the finding, "" when nowhere.
func jumpLabel(f health.Finding) string {
	switch {
	case len(f.Items) != 0:
		return fmt.Sprintf("list them (%d)", len(f.Items))
	case f.Check == "M1" || f.Check == "M3":
		return "links"
	case f.Check == "L3" || f.Check == "W8":
		return "migrate"
	case f.Check == "L6":
		return "trash"
	case f.Check == "A1" && f.Subject.Name != "Lua providers" && f.Subject.Name != "storage":
		return "accounts"
	case f.Subject.Kind == "watch":
		return "watch details"
	case f.Subject.Kind == "manga":
		return "library manga"
	}
	return ""
}

// applyLabel is what the apply key does on the finding, "" when nothing.
func applyLabel(f health.Finding) string {
	a := f.Fix.Action
	if a == nil {
		return ""
	}
	switch a.Kind {
	case health.ActionResume:
		return "resume"
	case health.ActionWatch:
		return "watch it"
	case health.ActionLink:
		name := sourceName(a.Source)
		if len(a.IDs) == 1 {
			return fmt.Sprintf("link %s %s", name, a.IDs[0])
		}
		return fmt.Sprintf("link %s %s", name, strings.Join(a.IDs, " or "))
	case health.ActionPick:
		return "pick " + strings.Join(a.IDs, " or ")
	case health.ActionMerge:
		return "merge"
	case health.ActionMark:
		if len(a.Mangas) > 1 {
			return fmt.Sprintf("mark %d folders", len(a.Mangas))
		}
		return "mark"
	case health.ActionForget:
		return "scan or forget"
	case health.ActionScan:
		return "scan"
	case health.ActionRename:
		if len(a.Paths) > 1 {
			return fmt.Sprintf("rename %d", len(a.Paths))
		}
		return "rename"
	case health.ActionEmptyTrash:
		return "empty"
	}
	return ""
}

func sourceName(code lmmeta.IDCode) string {
	if src, ok := client.MetadataSourceOf(code); ok {
		return src.Name
	}
	return string(code)
}

// state is the findings, or a summary finding's items (summary set): those don't reload.
type state struct {
	list.Screen
	report health.Report
	// network is true when the last run did the network checks
	network bool
	loaded  bool
	// category shows only its findings, "" all
	category health.Category
	summary  *health.Finding
	// applied are the library mangas whose watch was added here
	applied map[int64]bool
	// fixing is the finding whose library fix was asked last
	fixing *health.Finding
	// all shows the dismissed findings too
	all  bool
	keys keyMap
}

type keyMap struct {
	jump, apply, merge, info, network, refresh, next, dismiss, showAll key.Binding
}

// loadedMsg is a finished run.
type loadedMsg struct {
	report  health.Report
	network bool
}

// changedMsg is a fix applied: reload, then notify. applied is the library manga a watch
// was added for, 0 if none.
type changedMsg struct {
	notify  string
	applied int64
	// lib is a library fix: a summary drops the finding it was applied to
	lib bool
}

// runMsg is a confirmed change to the watches, run with Load by the State (a msg and not
// the dialog's cmd: a Load counts as busy from when it's made, the dialog's cmd may never
// run).
type runMsg struct {
	label   string
	applied int64
	do      func(context.Context, watch.Manager) (string, error)
}

// linkMsg opens the links screen of the library manga, planning the link.
type linkMsg struct {
	manga int64
	code  lmmeta.IDCode
	id    string
}

// pushMsg opens a screen once its data loaded.
type pushMsg struct{ state base.State }

// maxChoices is how many numbered choices a question has: one key each, 1 to 9.
const maxChoices = 9

// choiceMsg asks to choose (the link's id, the merge's keep), once its data loaded.
type choiceMsg struct {
	title, message string
	choices        []confirm.Choice
}

// New is the health screen.
func New() base.State {
	return newState(nil)
}

func newState(summary *health.Finding) *state {
	s := &state{
		summary: summary,
		applied: map[int64]bool{},
		keys: keyMap{
			jump:    util.Bind("open", "enter"),
			apply:   base.KeyApplyFix,
			merge:   base.KeyMerge,
			info:    base.KeyInfo,
			network: base.KeyNetwork,
			refresh: base.KeyRefresh,
			next:    base.KeyNext,
			dismiss: base.KeyDismiss,
			showAll: base.KeyShowDismissed,
		},
	}
	s.Screen = list.NewScreen(list.New(3, 1, "finding", "findings", []*item{}, func(i *item) _list.DefaultItem { return i }), s.keyMap)
	s.Confirm = confirm.NewDialog(50, color.Warning)
	if summary != nil {
		s.report = health.Report{Findings: summary.Items}
		s.loaded = true
		s.setItems()
	}
	return s
}

func (s *state) keyMap() help.KeyMap {
	k := s.keys
	if s.summary != nil {
		return util.KeyMap{Short: []key.Binding{k.jump, k.apply, k.info, k.dismiss}}
	}
	short := []key.Binding{k.jump, k.apply, k.merge, k.network, k.next, k.dismiss}
	return util.KeyMap{Short: short, Full: [][]key.Binding{append(short, k.info, k.refresh, k.showAll)}}
}

func (s *state) Key() string {
	if s.summary != nil {
		return "health/" + s.summary.Check + "/" + s.summary.Subject.String()
	}
	return "health"
}

func (s *state) Title() base.Title {
	if s.summary != nil {
		return base.Title{Text: "Health: " + s.summary.Check}
	}
	return base.Title{Text: "Health"}
}

func (s *state) Subtitle() string {
	parts := []string{s.List.Subtitle()}
	if s.summary != nil {
		return strings.Join(append(parts, s.summary.Message), " · ")
	}
	if s.category != "" {
		parts = append(parts, string(s.category))
	} else if s.loaded {
		parts = append(parts, fmt.Sprintf("all (%s: category)", s.keys.next.Help().Key))
	}
	if s.network {
		parts = append(parts, "with the network checks")
	} else if s.loaded {
		parts = append(parts, fmt.Sprintf("offline (%s: network checks)", s.keys.network.Help().Key))
	}
	return strings.Join(parts, " · ")
}

func (s *state) Status() string {
	if !s.loaded || s.summary != nil {
		return s.List.Status()
	}
	c := s.report.Counts
	status := fmt.Sprintf("%s, %s, %d info", stringutil.Quantify(c.Error, "error", "errors"), stringutil.Quantify(c.Warning, "warning", "warnings"), c.Info)
	if n := s.report.Dismissed; n > 0 {
		verb := "shows"
		if s.all {
			verb = "hides"
		}
		status += fmt.Sprintf(" · %d dismissed (%s %s them)", n, s.keys.showAll.Help().Key, verb)
	}
	if n := len(s.report.Errors); n > 0 {
		status += fmt.Sprintf(" · %d checks couldn't run (%s)", n, s.keys.info.Help().Key)
	}
	return status
}

func (s *state) Init(ctx context.Context) tea.Cmd {
	if s.summary != nil {
		return nil
	}
	return s.load(ctx, false)
}

// load runs the checks, the network ones too with network.
func (s *state) load(ctx context.Context, network bool) tea.Cmd {
	label := "Checking"
	if network {
		label = "Checking (network)"
	}
	all := s.all
	return base.LoadWithStatus(ctx, label, func(ctx context.Context, status func(string)) (loadedMsg, error) {
		checker, err := health.New(ctx)
		if err != nil {
			return loadedMsg{}, err
		}
		checker.OnProgress = status
		report, err := checker.Run(ctx, health.Options{Network: network, All: all})
		return loadedMsg{report: report, network: network}, err
	}, func(msg loadedMsg) tea.Msg { return msg })
}

func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	if cmd, ok := s.Answer(msg); ok {
		return cmd
	}
	switch msg := msg.(type) {
	case loadedMsg:
		s.onLoaded(msg)
		return s.setItems()
	case base.RestoredMsg:
		if s.summary != nil {
			return nil
		}
		// offline: the network findings stay while their manga isn't watched
		return s.load(ctx, false)
	case runMsg:
		return s.run(ctx, msg)
	case libMsg:
		return s.runLib(ctx, msg)
	case renamePlannedMsg:
		return s.askRename(msg)
	case trashCheckedMsg:
		return s.askEmptyTrash(msg)
	case linkMsg:
		return loadManga(ctx, msg.manga, func(m storage.Manga) (tea.Msg, error) {
			return pushMsg{state: tuilibrary.NewLinks(m, library.Links{msg.code: msg.id})}, nil
		})
	case dismissedMsg:
		return s.onDismissed(ctx, msg)
	case changedMsg:
		if msg.applied != 0 {
			s.applied[msg.applied] = true
		}
		if s.summary != nil {
			s.dropApplied()
			if msg.lib && s.fixing != nil {
				fixed := *s.fixing
				s.report.Findings = slices.DeleteFunc(s.report.Findings, func(f health.Finding) bool {
					return f.Check == fixed.Check && f.Subject == fixed.Subject && f.Message == fixed.Message
				})
			}
			return tea.Batch(s.setItems(), base.Notify(msg.notify))
		}
		return tea.Batch(s.load(ctx, false), base.Notify(msg.notify))
	case pushMsg:
		return func() tea.Msg { return msg.state }
	case choiceMsg:
		s.Confirm.Choose(msg.title, msg.message, s.View(), msg.choices...)
		return nil
	case tea.KeyMsg:
		if s.List.Filtering() {
			break
		}
		if cmd, ok := s.updateKey(ctx, msg); ok {
			return cmd
		}
	}
	return s.List.Update(msg)
}

// onLoaded keeps the run, with the network findings of the last network run that still
// hold when this one is offline (health.KeepNetwork).
func (s *state) onLoaded(msg loadedMsg) {
	if !msg.network && s.network {
		msg.report = health.KeepNetwork(s.report, msg.report)
		msg.network = true
	}
	s.report, s.network, s.loaded = msg.report, msg.network, true
	if s.category != "" && !slices.ContainsFunc(s.report.Findings, func(f health.Finding) bool { return f.Category == s.category }) {
		s.category = ""
	}
}

// dropApplied drops the summary's items whose watch was added (they're watched now).
func (s *state) dropApplied() {
	s.report.Findings = slices.DeleteFunc(s.report.Findings, func(f health.Finding) bool {
		return f.Fix.Action != nil && f.Fix.Action.Kind == health.ActionWatch && s.applied[f.Subject.ID]
	})
}

func (s *state) setItems() tea.Cmd {
	var items []_list.Item
	for _, f := range s.report.Findings {
		if s.category == "" || f.Category == s.category {
			items = append(items, &item{s: s, f: f})
		}
	}
	return s.List.SetItemsKeep(items)
}

// nextCategory is the category after the shown one that has findings, "" (all) after the
// last.
func (s *state) nextCategory() health.Category {
	i := slices.Index(health.Categories, s.category)
	for _, c := range health.Categories[i+1:] {
		if s.report.Counts.ByCategory[c] > 0 {
			return c
		}
	}
	return ""
}

func (s *state) updateKey(ctx context.Context, msg tea.KeyMsg) (tea.Cmd, bool) {
	if s.summary == nil {
		switch {
		case key.Matches(msg, s.keys.refresh):
			if base.Busy(ctx) {
				return nil, true
			}
			return s.load(ctx, s.network), true
		case key.Matches(msg, s.keys.network):
			if base.Busy(ctx) {
				return nil, true
			}
			return s.load(ctx, true), true
		case key.Matches(msg, s.keys.next):
			s.category = s.nextCategory()
			return s.setItems(), true
		case key.Matches(msg, s.keys.showAll):
			if base.Busy(ctx) {
				return nil, true
			}
			s.all = !s.all
			return s.load(ctx, s.network), true
		}
	}
	it, ok := s.List.SelectedItem().(*item)
	if !ok {
		if key.Matches(msg, s.keys.info) && len(s.report.Errors) != 0 {
			return base.Viewport("Health", "Couldn't run:\n  "+strings.Join(s.report.Errors, "\n  "), color.Warning), true
		}
		return nil, false
	}
	f := it.f
	switch {
	case key.Matches(msg, s.keys.jump):
		return s.jump(ctx, f), true
	case key.Matches(msg, s.keys.info):
		return base.Viewport(f.Subject.String(), s.info(f), severityColor(f.Severity)), true
	case key.Matches(msg, s.keys.apply):
		return s.apply(ctx, f), true
	case key.Matches(msg, s.keys.dismiss):
		return s.dismiss(ctx, f), true
	case key.Matches(msg, s.keys.merge):
		if a := f.Fix.Action; a != nil && a.Kind == health.ActionMerge {
			return s.merge(ctx, a), true
		}
		return base.Notify("Not a duplicate"), true
	}
	return nil, false
}

func severityColor(sev health.Severity) lipgloss.Color {
	switch sev {
	case health.Error:
		return color.Error
	case health.Warning:
		return color.Warning
	}
	return color.Accent
}

// info is the whole finding: details, fix commands and note, and the run's errors.
func (s *state) info(f health.Finding) string {
	lines := []string{fmt.Sprintf("%s (%s, %s): %s", f.Check, f.Category, f.Severity, f.Message)}
	for _, d := range f.Details {
		lines = append(lines, "  "+d)
	}
	for _, c := range f.Fix.Commands {
		lines = append(lines, "fix: "+c)
	}
	if f.Fix.Note != "" {
		lines = append(lines, "("+f.Fix.Note+")")
	}
	if f.Fix.TUI != "" {
		lines = append(lines, "in the TUI: "+f.Fix.TUI)
	}
	if len(s.report.Errors) != 0 {
		lines = append(lines, "", "Couldn't run:")
		for _, e := range s.report.Errors {
			lines = append(lines, "  "+e)
		}
	}
	return strings.Join(lines, "\n")
}

// jump opens the screen of the finding.
func (s *state) jump(ctx context.Context, f health.Finding) tea.Cmd {
	switch what := jumpLabel(f); {
	case what == "":
		return base.Notify("Nothing to open")
	case len(f.Items) != 0:
		summary := f
		return func() tea.Msg { return newState(&summary) }
	case what == "migrate":
		return func() tea.Msg { return migrate.New() }
	case what == "accounts":
		return func() tea.Msg { return accounts.New(false) }
	case what == "trash":
		return func() tea.Msg {
			state, err := tuilibrary.NewTrash()
			if err != nil {
				return err
			}
			return state
		}
	case what == "links":
		return loadManga(ctx, f.Subject.ID, func(m storage.Manga) (tea.Msg, error) {
			return pushMsg{state: tuilibrary.NewLinks(m, nil)}, nil
		})
	case what == "watch details":
		id := f.Subject.ID
		return base.Load(ctx, "Loading the watch", func(ctx context.Context) (pushMsg, error) {
			m, err := watches.Connect(ctx)
			if err != nil {
				return pushMsg{}, err
			}
			w, err := m.Get(ctx, id)
			if err != nil {
				return pushMsg{}, err
			}
			return pushMsg{state: watches.NewDetails(w)}, nil
		}, func(msg pushMsg) tea.Msg { return msg })
	default:
		return loadManga(ctx, f.Subject.ID, func(m storage.Manga) (tea.Msg, error) {
			state, err := tuilibrary.NewManga(m)
			return pushMsg{state: state}, err
		})
	}
}

// loadManga loads the library manga, then makes the msg of it.
func loadManga(ctx context.Context, id int64, then func(storage.Manga) (tea.Msg, error)) tea.Cmd {
	return base.Load(ctx, "Loading the manga", func(context.Context) (tea.Msg, error) {
		lib, err := cache.Library()
		if err != nil {
			return nil, err
		}
		m, ok, err := lib.MangaByID(id)
		if err != nil {
			return nil, err
		}
		if !ok {
			return base.NotificationMsg{Message: fmt.Sprintf("Library manga %d is gone", id)}, nil
		}
		return then(m)
	}, func(msg tea.Msg) tea.Msg { return msg })
}

// apply applies the finding's fix, after a confirm (the links' and merge's screens ask
// theirs).
func (s *state) apply(ctx context.Context, f health.Finding) tea.Cmd {
	a := f.Fix.Action
	if a == nil {
		if len(f.Fix.Commands) != 0 {
			return base.Notify("No fix to apply here, run: " + f.Fix.Commands[0])
		}
		return base.Notify("No fix to apply here")
	}
	if base.Busy(ctx) {
		return nil
	}
	switch a.Kind {
	case health.ActionResume:
		id := a.Watch
		s.Confirm.Ask("Resume", fmt.Sprintf("Resume watch %d (%s)?", id, f.Subject.Name), s.View(), change("Resuming", 0, func(ctx context.Context, m watch.Manager) (string, error) {
			w, err := m.SetEnabled(ctx, id, true)
			return "resumed " + w.Title, err
		}))
	case health.ActionWatch:
		title, opts := f.Subject.Name, watch.AddOptions{Provider: a.Provider, MangaID: a.MangaID}
		message := fmt.Sprintf("Watch %q on %s? Its first check downloads the chapters after the library's last.", title, a.Provider)
		s.Confirm.Ask("Watch", message, s.View(), change("Adding the watch", f.Subject.ID, func(ctx context.Context, m watch.Manager) (string, error) {
			w, err := m.Add(ctx, opts)
			return fmt.Sprintf("watching %s on %s", w.Title, w.Provider), err
		}))
	case health.ActionLink:
		manga, code := a.Manga, a.Source
		if len(a.IDs) == 1 {
			id := a.IDs[0]
			return func() tea.Msg { return linkMsg{manga: manga, code: code, id: id} }
		}
		var choices []confirm.Choice
		ids := a.IDs[:min(len(a.IDs), maxChoices)]
		for i, id := range ids {
			choices = append(choices, confirm.Choice{Key: strconv.Itoa(i + 1), Label: fmt.Sprintf("link %s %s", sourceName(code), id),
				Cmd: func() tea.Msg { return linkMsg{manga: manga, code: code, id: id} }})
		}
		message := f.Message + "\n\nWhich one? (the link's plan comes next)"
		if n := len(a.IDs) - len(ids); n > 0 {
			message += fmt.Sprintf("\n(%d more not shown, link one from the links screen: enter)", n)
		}
		s.Confirm.Choose("Link", message, s.View(), choices...)
	case health.ActionPick:
		source := a.Source
		return loadManga(ctx, a.Manga, func(m storage.Manga) (tea.Msg, error) {
			return pushMsg{state: tuilibrary.NewLinksPick(m, source)}, nil
		})
	case health.ActionMerge:
		return s.merge(ctx, a)
	default:
		s.fixing = &f
		cmd, _ := s.applyLibrary(ctx, f)
		return cmd
	}
	return nil
}

// change is the cmd of a confirmed change to the watches (runMsg).
func change(label string, applied int64, do func(context.Context, watch.Manager) (string, error)) tea.Cmd {
	return func() tea.Msg { return runMsg{label: label, applied: applied, do: do} }
}

// run makes the change, changedMsg after it.
func (s *state) run(ctx context.Context, msg runMsg) tea.Cmd {
	return base.Load(ctx, msg.label, func(ctx context.Context) (changedMsg, error) {
		m, err := watches.Connect(ctx)
		if err != nil {
			return changedMsg{}, err
		}
		notify, err := msg.do(ctx, m)
		return changedMsg{notify: notify, applied: msg.applied}, err
	}, func(msg changedMsg) tea.Msg { return msg })
}

// merge asks which of the duplicates to keep (the suggested one first), then opens the
// library's merge of the other into it (its plan and confirm).
func (s *state) merge(ctx context.Context, a *health.Action) tea.Cmd {
	ids := a.Mangas
	return base.Load(ctx, "Loading the duplicates", func(context.Context) (tea.Msg, error) {
		lib, err := cache.Library()
		if err != nil {
			return nil, err
		}
		var mangas []storage.Manga
		for _, id := range ids {
			if m, ok, err := lib.MangaByID(id); err != nil {
				return nil, err
			} else if ok {
				mangas = append(mangas, m)
			}
		}
		if len(mangas) < 2 {
			return base.NotificationMsg{Message: "Nothing to merge any more"}, nil
		}
		lines := []string{"Merge them into one, which one stays? (the merge's plan comes next)"}
		var choices []confirm.Choice
		// a key each: the first ones, the next merges are asked again
		for i, keep := range mangas[:min(len(mangas), maxChoices)] {
			other := mangas[0]
			if i == 0 {
				other = mangas[1]
			}
			line := fmt.Sprintf("%d. %s  %d chapters  %s", i+1, keep.Title, keep.Chapters, keep.Path())
			if i == 0 {
				line += " (suggested)"
			}
			lines = append(lines, line)
			keepID, otherID := keep.ID, other.ID
			choices = append(choices, confirm.Choice{Key: strconv.Itoa(i + 1), Label: fmt.Sprintf("keep %d", keep.ID),
				Cmd: func() tea.Msg { return tuilibrary.NewMerge(keepID, otherID) }})
		}
		if len(mangas) > 2 {
			lines = append(lines, fmt.Sprintf("(%d folders: this merges two, the finding stays for the rest)", len(mangas)))
		}
		return choiceMsg{title: "Merge", message: strings.Join(lines, "\n"), choices: choices}, nil
	}, func(msg tea.Msg) tea.Msg { return msg })
}

func (s *state) View() string {
	if s.Asking() {
		return s.Confirm.View()
	}
	if s.loaded && len(s.List.Items()) == 0 && s.List.Unfiltered() {
		return "No problems found."
	}
	return s.List.View()
}

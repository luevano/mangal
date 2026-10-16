package health

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/health"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/confirm"
	tuilibrary "github.com/luevano/mangal/v5/tui/state/library"
)

// libMsg is a confirmed change to the library, run with Load by the State (as runMsg).
type libMsg struct {
	label string
	do    func(ctx context.Context, status func(string)) (string, error)
}

func libChange(label string, do func(ctx context.Context, status func(string)) (string, error)) tea.Cmd {
	return func() tea.Msg { return libMsg{label: label, do: do} }
}

func (s *state) runLib(ctx context.Context, msg libMsg) tea.Cmd {
	return base.LoadWithStatus(ctx, msg.label, func(ctx context.Context, status func(string)) (changedMsg, error) {
		notify, err := msg.do(ctx, status)
		return changedMsg{notify: notify, lib: true}, err
	}, func(msg changedMsg) tea.Msg { return msg })
}

// undoable ends the notification of a journaled change.
var undoable = " (" + base.KeyUndo.Help().Key + " in the library undoes it)"

// fixMsg is a confirmed fix, applied by the core with Load by the State (as runMsg).
// watch is a fix of the watches (resume, watch it), applied the library manga a watch
// is added for.
type fixMsg struct {
	label   string
	f       health.Finding
	watch   bool
	applied int64
}

func fix(label string, f health.Finding, watch bool, applied int64) tea.Cmd {
	return func() tea.Msg { return fixMsg{label: label, f: f, watch: watch, applied: applied} }
}

// fixFinding plans the finding's fix (core.HealthFixReq), applies it with apply when
// there's something to do.
func fixFinding(ctx context.Context, f health.Finding, apply bool, status func(string)) (health.FixResult, error) {
	changes, err := base.Changes()
	if err != nil {
		return health.FixResult{}, err
	}
	if status != nil {
		ctx = base.WithStatus(ctx, status)
	} else {
		status = func(string) {}
	}
	plan, err := changes.Plan(ctx, core.HealthFixReq{Finding: f})
	if err != nil {
		return health.FixResult{}, err
	}
	if !apply || plan.Nothing {
		return plan.Detail.(health.FixResult), nil
	}
	res, err := changes.ApplyPlan(ctx, plan, base.ApplyOpts(status))
	if err != nil {
		return health.FixResult{}, err
	}
	return res.Detail.(health.FixResult), nil
}

// runFix applies the fix, changedMsg after it.
func (s *state) runFix(ctx context.Context, msg fixMsg) tea.Cmd {
	return base.LoadWithStatus(ctx, msg.label, func(ctx context.Context, status func(string)) (changedMsg, error) {
		res, err := fixFinding(ctx, msg.f, true, status)
		if err != nil {
			return changedMsg{}, err
		}
		notify := res.Message
		if res.Snapshot != "" {
			notify += undoable
		}
		return changedMsg{notify: notify, applied: msg.applied, lib: !msg.watch}, nil
	}, func(msg changedMsg) tea.Msg { return msg })
}

// fixPlannedMsg is a finding's fix planned again, to confirm.
type fixPlannedMsg struct {
	f   health.Finding
	res health.FixResult
}

// applyLibrary asks to apply a library fix (mark, forget, scan, rename, backups, empty the trash),
// false when the action isn't one. The Fixer applies them but the scan and emptying the
// trash, it refuses those.
func (s *state) applyLibrary(ctx context.Context, f health.Finding) (tea.Cmd, bool) {
	a := f.Fix.Action
	switch a.Kind {
	case health.ActionMark:
		n := len(a.Mangas)
		if a.Manga != 0 {
			n = 1
		}
		message := fmt.Sprintf("Write the .libmangal.json of %q from the library?", f.Subject.Name)
		if n != 1 {
			message = fmt.Sprintf("Write the .libmangal.json of the %d folders from the library (what \"mangal library scan --mark --apply\" writes)?", n)
		}
		s.Confirm.Ask("Mark", message, s.View(), fix("Marking", f, false, 0))
	case health.ActionForget:
		s.Confirm.Choose("Library manga", fmt.Sprintf("%s: %s\n\nScan the library (it finds folders that moved), or forget the manga?", f.Subject, f.Message), s.View(),
			confirm.Choice{Key: "u", Label: "scan the library", Cmd: libChange("Scanning the library", scanLibrary)},
			confirm.Choice{Key: "d", Label: "forget it", Cmd: fix("Forgetting "+f.Subject.Name, f, false, 0)})
	case health.ActionScan:
		s.Confirm.Ask("Scan", "Scan the library? It adds the folders it doesn't have and finds the ones that moved.", s.View(),
			libChange("Scanning the library", scanLibrary))
	case health.ActionBackups:
		return base.Load(ctx, "Planning the links", func(ctx context.Context) (fixPlannedMsg, error) {
			res, err := fixFinding(ctx, f, false, nil)
			return fixPlannedMsg{f: f, res: res}, err
		}, func(msg fixPlannedMsg) tea.Msg { return msg }), true
	case health.ActionRename:
		return base.Load(ctx, "Planning the renames", func(ctx context.Context) (fixPlannedMsg, error) {
			res, err := fixFinding(ctx, f, false, nil)
			return fixPlannedMsg{f: f, res: res}, err
		}, func(msg fixPlannedMsg) tea.Msg { return msg }), true
	case health.ActionEmptyTrash:
		message := f.Message
		return base.PlanChange(ctx, "Checking the undos", core.EmptyTrashReq{}, func(p core.Plan) tea.Msg {
			return trashCheckedMsg{message: message, plan: p}
		}), true
	default:
		return nil, false
	}
	return nil, true
}

// trashCheckedMsg is a finding's message and emptying the trash planned, to confirm.
type trashCheckedMsg struct {
	message string
	plan    core.Plan
}

// askEmptyTrash asks to empty the trash.
func (s *state) askEmptyTrash(msg trashCheckedMsg) tea.Cmd {
	if msg.plan.Nothing {
		return base.Notify("The trash is empty")
	}
	question := tuilibrary.EmptyTrashQuestion("Delete them for good? It can't be undone.", tuilibrary.TrashCheckedMsg{Plan: msg.plan})
	plan := msg.plan
	s.Confirm.AskDelete("Empty the trash", msg.message+"\n\n"+question, s.View(),
		libChange("Emptying the trash", func(ctx context.Context, status func(string)) (string, error) {
			return tuilibrary.EmptyTrash(ctx, plan, status)
		}))
	return nil
}

// askPlanned asks to apply the planned fix (the renames, the backup links).
func (s *state) askPlanned(msg fixPlannedMsg) tea.Cmd {
	if msg.res.Nothing {
		return base.Notify(capitalize(msg.res.Message))
	}
	title, question, label := "Rename", "Rename?", "Renaming"
	if msg.res.Kind == health.ActionBackups {
		title, question, label = "Link", "Set these links?", "Linking"
	}
	s.Confirm.Ask(title, strings.Join(append(msg.res.Plan, "", question), "\n"), s.View(), fix(label, msg.f, false, 0))
	return nil
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// scanLibrary scans the library, the notification says what it found.
func scanLibrary(ctx context.Context, status func(string)) (string, error) {
	c, err := base.Core()
	if err != nil {
		return "", err
	}
	rep, err := c.Library.Scan(base.WithStatus(ctx, status), core.ScanOptions{})
	if err != nil {
		return "", err
	}
	res := rep.Result
	notify := fmt.Sprintf("scanned: %d chapters added, %d removed, %d folders new, %d moved", res.Added, res.Removed, res.New, res.Relocated)
	if n := len(res.Missing); n != 0 {
		notify += fmt.Sprintf(", %d still missing", n)
		if rep.OtherRoot != "" {
			notify += fmt.Sprintf(" (they're in %s: the library's scan offers to use it as download.path)", rep.OtherRoot)
		}
	}
	return notify, nil
}

// dismissedMsg is a finding dismissed (or brought back).
type dismissedMsg struct {
	key       string
	dismissed bool
}

// dismiss dismisses the finding (fine as it is), or brings a dismissed one back.
func (s *state) dismiss(ctx context.Context, f health.Finding) tea.Cmd {
	if f.Key == "" {
		return base.Notify("This finding can't be dismissed")
	}
	return base.Load(ctx, "", func(ctx context.Context) (dismissedMsg, error) {
		c, err := base.Core()
		if err != nil {
			return dismissedMsg{}, err
		}
		if f.Dismissed {
			// already back is fine
			if err := c.Health.Undismiss(ctx, f.Key); err != nil && !errors.Is(err, core.ErrNotFound) {
				return dismissedMsg{}, err
			}
			return dismissedMsg{key: f.Key}, nil
		}
		_, err = c.Health.Dismiss(ctx, core.DismissReq{Keys: []string{f.Key}, Known: []health.Finding{f}})
		return dismissedMsg{key: f.Key, dismissed: true}, err
	}, func(msg dismissedMsg) tea.Msg { return msg })
}

// onDismissed reloads (a summary updates its items), then notifies.
func (s *state) onDismissed(ctx context.Context, msg dismissedMsg) tea.Cmd {
	notify := "dismissed " + msg.key + " (" + s.keys.showAll.Help().Key + " shows the dismissed)"
	if !msg.dismissed {
		notify = "brought back " + msg.key
	}
	if s.summary == nil {
		return tea.Batch(s.load(ctx, false), base.Notify(notify))
	}
	for i, f := range s.report.Findings {
		if f.Key == msg.key {
			s.report.Findings[i].Dismissed = msg.dismissed
		}
	}
	if msg.dismissed {
		s.report.Findings = slices.DeleteFunc(s.report.Findings, func(f health.Finding) bool { return f.Key == msg.key })
	}
	return tea.Batch(s.setItems(), base.Notify(notify))
}

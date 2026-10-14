package health

import (
	"context"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/client"
	watchpkg "github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/health"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/confirm"
	tuilibrary "github.com/luevano/mangal/v5/tui/state/library"
	"github.com/luevano/mangal/v5/tui/state/watches"
	"github.com/luevano/mangal/v5/util/cache"
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

// newFixer is the Fixer of the fixes (the serve API's), over the watches of m (nil for
// the library fixes). Swapped in tests.
var newFixer = health.NewFixer

// fixMsg is a confirmed fix, applied by the Fixer with Load by the State (as runMsg).
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

// fixFinding plans the finding's fix with the Fixer, applies it with apply.
func fixFinding(ctx context.Context, f health.Finding, watch, apply bool, status func(string)) (health.FixResult, error) {
	var m watchpkg.Manager
	if watch {
		var err error
		if m, err = watches.Connect(ctx); err != nil {
			return health.FixResult{}, err
		}
	}
	x, err := newFixer(m)
	if err != nil {
		return health.FixResult{}, err
	}
	x.OnProgress = status
	return x.Fix(ctx, f, "", apply)
}

// runFix applies the fix, changedMsg after it.
func (s *state) runFix(ctx context.Context, msg fixMsg) tea.Cmd {
	return base.LoadWithStatus(ctx, msg.label, func(ctx context.Context, status func(string)) (changedMsg, error) {
		res, err := fixFinding(ctx, msg.f, msg.watch, true, status)
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

// applyLibrary asks to apply a library fix (mark, forget, scan, rename, empty the trash),
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
	case health.ActionRename:
		return base.Load(ctx, "Planning the renames", func(ctx context.Context) (fixPlannedMsg, error) {
			res, err := fixFinding(ctx, f, false, false, nil)
			return fixPlannedMsg{f: f, res: res}, err
		}, func(msg fixPlannedMsg) tea.Msg { return msg }), true
	case health.ActionEmptyTrash:
		message := f.Message
		return base.Load(ctx, "Checking the undos", func(context.Context) (trashCheckedMsg, error) {
			blocked, err := tuilibrary.TrashBlocks(path.Downloads())
			return trashCheckedMsg{message: message, blocked: blocked}, err
		}, func(msg trashCheckedMsg) tea.Msg { return msg }), true
	default:
		return nil, false
	}
	return nil, true
}

// trashCheckedMsg is a finding's message and the undos emptying the trash blocks, to confirm.
type trashCheckedMsg struct {
	message string
	blocked tuilibrary.TrashCheckedMsg
}

// askEmptyTrash asks to empty the trash.
func (s *state) askEmptyTrash(msg trashCheckedMsg) tea.Cmd {
	question := tuilibrary.EmptyTrashQuestion("Delete them for good? It can't be undone.", msg.blocked)
	s.Confirm.AskDelete("Empty the trash", msg.message+"\n\n"+question, s.View(),
		libChange("Emptying the trash", func(context.Context, func(string)) (string, error) {
			files, err := client.LibraryFiles()
			if err != nil {
				return "", err
			}
			return tuilibrary.EmptyTrash(files)
		}))
	return nil
}

// askRename asks to do the planned renames.
func (s *state) askRename(msg fixPlannedMsg) tea.Cmd {
	if msg.res.Nothing {
		return base.Notify(capitalize(msg.res.Message))
	}
	s.Confirm.Ask("Rename", strings.Join(append(msg.res.Plan, "", "Rename?"), "\n"), s.View(), fix("Renaming", msg.f, false, 0))
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
	files, err := client.LibraryFiles()
	if err != nil {
		return "", err
	}
	providerDirs := config.Download.Provider.CreateDir.Get()
	res, otherRoot, err := tuilibrary.Scan(ctx, files, providerDirs, status)
	if err != nil {
		return "", err
	}
	notify := fmt.Sprintf("scanned: %d chapters added, %d removed, %d folders new, %d moved", res.Added, res.Removed, res.New, res.Relocated)
	if n := len(res.Missing); n != 0 {
		notify += fmt.Sprintf(", %d still missing", n)
		if otherRoot != "" {
			notify += fmt.Sprintf(" (they're in %s: the library's scan offers to use it as download.path)", otherRoot)
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
	return base.Load(ctx, "", func(context.Context) (dismissedMsg, error) {
		check, subject, err := health.SplitKey(f.Key)
		if err != nil {
			return dismissedMsg{}, err
		}
		st, err := cache.Storage()
		if err != nil {
			return dismissedMsg{}, err
		}
		ignores := storage.HealthIgnores{DB: st.Data}
		if f.Dismissed {
			_, err = ignores.Undismiss(check, subject)
			return dismissedMsg{key: f.Key}, err
		}
		err = ignores.Dismiss(storage.HealthIgnore{Check: check, Subject: subject, Message: f.Message})
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
